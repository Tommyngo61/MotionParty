(function () {
  const STYLE_ID = 'cmc-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .cmc-wrap{position:relative;width:100%;min-height:82vh;display:flex;flex-direction:column;
        border-radius:20px;overflow:hidden;background:#0d1024}
      .cmc-hud{display:flex;justify-content:center;align-items:center;padding:.6rem 1rem;
        background:rgba(0,0,0,.35);color:#fff;font-weight:800;z-index:4}
      .cmc-stage{position:relative;flex:1;background:#000;overflow:hidden}
      .cmc-video{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;transform:scaleX(-1) scaleX(1)}
      .cmc-reticle{position:absolute;top:50%;left:50%;width:26%;height:26%;transform:translate(-50%,-50%);
        border:3px solid rgba(255,255,255,.85);border-radius:12px;box-shadow:0 0 0 999px rgba(0,0,0,.25);
        pointer-events:none}
      .cmc-target{position:absolute;top:.8rem;left:50%;transform:translateX(-50%);display:flex;align-items:center;
        gap:.5rem;background:rgba(0,0,0,.55);padding:.4rem .8rem;border-radius:12px;z-index:3}
      .cmc-target-swatch{width:28px;height:28px;border-radius:50%;border:2px solid #fff;flex-shrink:0}
      .cmc-target-name{color:#fff;font-weight:800}
      .cmc-capture-btn{margin:1rem;font-size:1.3rem}
      .cmc-capture-btn:disabled{opacity:.4}
      .cmc-feedback{position:absolute;bottom:1rem;left:50%;transform:translateX(-50%) translateY(20px);
        display:flex;align-items:center;gap:.5rem;background:rgba(0,0,0,.7);padding:.5rem 1rem;border-radius:12px;
        opacity:0;transition:opacity .2s,transform .2s;z-index:3;pointer-events:none}
      .cmc-feedback.show{opacity:1;transform:translateX(-50%) translateY(0)}
      .cmc-feedback-swatch{width:22px;height:22px;border-radius:50%;border:2px solid #fff}
      .cmc-feedback-text{color:#fff;font-weight:800}
      .cmc-feedback.good .cmc-feedback-text{color:#3ddc84}
      .cmc-msg{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
        flex-direction:column;gap:.7rem;text-align:center;padding:1.5rem;background:rgba(10,6,26,0);color:#fff;
        pointer-events:none;transition:background .15s;z-index:5}
      .cmc-msg.show{background:rgba(10,6,26,.8);pointer-events:auto}
      .cmc-msg .big{font-size:1.7rem;font-weight:900;text-shadow:0 3px 0 rgba(0,0,0,.4)}
      .cmc-msg .sub{color:rgba(255,255,255,.8)}
    `;
    document.head.appendChild(style);
  }

  const MATCH_THRESHOLD = 110; // mirrors server's CM_MATCH_THRESHOLD
  const SAMPLE_FRAC = 0.26; // center sample square, as a fraction of the shorter video dimension - matches .cmc-reticle

  function start(root, ctx) {
    ensureStyle();
    const { socket, opponents, playerId } = ctx;
    const { hexToRgb, rgbToHex, colorDistance, similarityPct } = window.MP_COLORS;

    root.innerHTML = `
      <div class="cmc-wrap">
        <div class="cmc-hud"><span id="cmc-timer">Get ready…</span></div>
        <div class="cmc-stage">
          <video id="cmc-video" class="cmc-video" autoplay playsinline muted></video>
          <div class="cmc-reticle"></div>
          <div class="cmc-target" id="cmc-target" style="display:none">
            <div class="cmc-target-swatch" id="cmc-target-swatch"></div>
            <div class="cmc-target-name" id="cmc-target-name"></div>
          </div>
          <div class="cmc-feedback" id="cmc-feedback"><div class="cmc-feedback-swatch" id="cmc-feedback-swatch"></div><div class="cmc-feedback-text" id="cmc-feedback-text"></div></div>
          <div class="cmc-msg show" id="cmc-msg">
            <div class="big" id="cmc-msg-big"></div>
            <div class="sub" id="cmc-msg-sub"></div>
            <button id="cmc-enable" class="btn primary">📷 Enable Camera</button>
          </div>
        </div>
        <button id="cmc-capture" class="btn primary big cmc-capture-btn" disabled>📸 Capture!</button>
      </div>
    `;

    const video = root.querySelector('#cmc-video');
    const canvas = document.createElement('canvas');
    const c2d = canvas.getContext('2d', { willReadFrequently: true });
    const targetBox = root.querySelector('#cmc-target');
    const targetSwatch = root.querySelector('#cmc-target-swatch');
    const targetName = root.querySelector('#cmc-target-name');
    const timerLabel = root.querySelector('#cmc-timer');
    const captureBtn = root.querySelector('#cmc-capture');
    const feedback = root.querySelector('#cmc-feedback');
    const feedbackSwatch = root.querySelector('#cmc-feedback-swatch');
    const feedbackText = root.querySelector('#cmc-feedback-text');
    const msgBox = root.querySelector('#cmc-msg');
    const msgBig = root.querySelector('#cmc-msg-big');
    const msgSub = root.querySelector('#cmc-msg-sub');
    const enableBtn = root.querySelector('#cmc-enable');

    function setMsg(big, sub, show, showEnable) {
      msgBig.textContent = big || '';
      msgSub.textContent = sub || '';
      enableBtn.style.display = showEnable ? 'inline-block' : 'none';
      msgBox.classList.toggle('show', !!show);
    }

    const raceIntro = opponents.length === 0 ? 'Get ready to match a color'
      : opponents.length === 1 ? `Racing ${opponents[0].name} to match the color`
      : `Racing ${opponents.length} others to match the color`;
    setMsg('Point your camera at things!', raceIntro, true, true);

    let state = 'permission'; // permission | ready | racing | waiting-result | finished
    let stream = null;
    let targetRgb = null;
    let roundEndsAt = 0;
    let rafId = null;
    let feedbackTimer = null;

    // Camera access needs an explicit tap to reliably prompt on iOS Safari, same
    // reasoning as the motion-permission request on the join screen.
    async function enableCamera() {
      enableBtn.disabled = true;
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false });
        video.srcObject = stream;
        await video.play();
        if (roundEndsAt > performance.now()) {
          // a round is already under way - jump straight in
          state = 'racing';
          captureBtn.disabled = false;
          setMsg('', '', false, false);
        } else {
          state = 'ready';
          setMsg('Get ready…', 'Waiting for the color…', true, false);
        }
      } catch (e) {
        enableBtn.disabled = false;
        setMsg('Camera blocked', 'Allow camera access for this site in your browser settings, then tap again.', true, true);
      }
    }
    enableBtn.addEventListener('click', enableCamera);

    function onGo({ target, roundMs }) {
      targetRgb = hexToRgb(target.hex);
      targetSwatch.style.background = target.hex;
      targetName.textContent = target.name;
      targetBox.style.display = 'flex';
      roundEndsAt = performance.now() + roundMs;
      if (state === 'permission') {
        // Camera was never enabled before the round started - keep the prompt up
        // (instead of silently locking them out with no explanation) so they can
        // still jump in the instant they grant it; the timer runs regardless.
        setMsg('Quick, enable your camera!', 'Find something the target color and photograph it before time runs out.', true, true);
      } else {
        state = 'racing';
        captureBtn.disabled = false;
        setMsg('', '', false, false);
      }
      tick();
    }
    socket.on('colormatch:go', onGo);

    function tick() {
      if (state !== 'racing' && state !== 'permission') return;
      const remain = Math.max(0, roundEndsAt - performance.now());
      timerLabel.textContent = (remain / 1000).toFixed(1) + 's';
      if (remain <= 0) {
        captureBtn.disabled = true;
        state = 'waiting-result';
        setMsg("Time's up!", 'Waiting for the result…', true, false);
        return;
      }
      rafId = requestAnimationFrame(tick);
    }

    function captureAndSubmit() {
      if (state !== 'racing' || !targetRgb) return;
      const vw = video.videoWidth, vh = video.videoHeight;
      if (!vw || !vh) return;
      canvas.width = vw;
      canvas.height = vh;
      c2d.drawImage(video, 0, 0, vw, vh);
      const size = Math.max(4, Math.round(Math.min(vw, vh) * SAMPLE_FRAC));
      const sx = Math.round(vw / 2 - size / 2);
      const sy = Math.round(vh / 2 - size / 2);
      const data = c2d.getImageData(sx, sy, size, size).data;
      let r = 0, g = 0, b = 0;
      const pixels = data.length / 4;
      for (let i = 0; i < data.length; i += 4) {
        r += data[i]; g += data[i + 1]; b += data[i + 2];
      }
      const capturedRgb = { r: Math.round(r / pixels), g: Math.round(g / pixels), b: Math.round(b / pixels) };
      const capturedHex = rgbToHex(capturedRgb);
      const distance = colorDistance(capturedRgb, targetRgb);
      const pct = similarityPct(distance);
      const matched = distance <= MATCH_THRESHOLD;

      feedbackSwatch.style.background = capturedHex;
      feedbackText.textContent = matched ? `🎯 ${pct}% match!` : `${pct}% match — try again!`;
      feedback.classList.toggle('good', matched);
      feedback.classList.add('show');
      clearTimeout(feedbackTimer);
      feedbackTimer = setTimeout(() => feedback.classList.remove('show'), 1400);
      if (navigator.vibrate) navigator.vibrate(matched ? [0, 60] : [0, 25]);

      socket.emit('player:input', { gameEvent: 'colormatch:attempt', payload: { hex: capturedHex, distance: Math.round(distance) } });
      socket.emit('colormatch:submit', { hex: capturedHex, distance });

      if (matched) {
        captureBtn.disabled = true;
        setMsg('Match found!', 'Waiting for the result…', true, false);
      }
    }
    captureBtn.addEventListener('click', captureAndSubmit);

    function onResult(result) {
      state = 'finished';
      captureBtn.disabled = true;
      const won = result.winnerId === playerId;
      if (won) {
        setMsg('🏆 YOU WIN!', result.winnerDistance != null ? `Best match: ${similarityPct(result.winnerDistance)}%` : '', true, false);
      } else if (result.winnerId) {
        const winner = opponents.find((p) => p.id === result.winnerId);
        setMsg('Someone else matched better', `${winner ? winner.name : 'Another player'} had the closest color.`, true, false);
      } else {
        setMsg('Nobody found a match!', "Time ran out before anyone got close enough.", true, false);
      }
      if (navigator.vibrate) navigator.vibrate(won ? [0, 60] : [0, 120, 60, 120]);
    }
    socket.on('colormatch:result', onResult);

    return {
      stop() {
        cancelAnimationFrame(rafId);
        clearTimeout(feedbackTimer);
        if (stream) stream.getTracks().forEach((t) => t.stop());
        socket.off('colormatch:go', onGo);
        socket.off('colormatch:result', onResult);
      },
    };
  }

  window.MP_CONTROLLERS = window.MP_CONTROLLERS || {};
  window.MP_CONTROLLERS.colormatch = { start };
})();
