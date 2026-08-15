// Shared "How to Play" modal. Each game registers itself into window.MP_TUTORIALS
// (same registry pattern as MP_GAMES / MP_CONTROLLERS) with an emoji, title, a
// list of step strings, and an animate(stageEl) function that renders a small
// looping CSS demo of the actual motion - there's no real video to show, so this
// stands in for one. animate() must return a stop() function to clean up timers/
// listeners when the modal closes.
//
// Registration shape:
//   window.MP_TUTORIALS.<gameId> = {
//     emoji: '🎾', title: 'Motion Tennis',
//     steps: ['Step one…', 'Step two…'],
//     animate(stageEl) { ...; return () => { /* stop/cleanup */ }; },
//   };
(function () {
  const STYLE_ID = 'mpt-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .mpt-overlay{position:fixed;inset:0;background:rgba(10,6,26,.72);z-index:100;
        display:flex;align-items:center;justify-content:center;padding:2rem;animation:mpt-fade .15s ease}
      @keyframes mpt-fade{from{opacity:0}to{opacity:1}}
      .mpt-card{background:#fff;color:#16233d;border-radius:22px;max-width:560px;width:100%;
        max-height:88vh;overflow-y:auto;padding:1.6rem;box-shadow:0 20px 50px rgba(0,0,0,.4)}
      .mpt-head{display:flex;align-items:center;gap:.6rem;margin-bottom:.8rem}
      .mpt-emoji{font-size:1.8rem}
      .mpt-title{font-size:1.3rem;font-weight:900;flex:1}
      .mpt-close{background:none;border:none;font-size:1.3rem;cursor:pointer;color:rgba(22,35,61,.5);
        line-height:1;padding:.3rem}
      .mpt-close:hover{color:#16233d}
      .mpt-stage{background:radial-gradient(circle at 50% 20%,#eaf6ff,#dcefff 70%);border-radius:16px;
        height:180px;position:relative;overflow:hidden;margin-bottom:1.2rem}
      .mpt-steps{margin:0 0 1.4rem;padding-left:1.3rem;display:flex;flex-direction:column;gap:.5rem}
      .mpt-steps li{line-height:1.4}
      .mpt-gotit{width:100%}
      /* Shared phone glyph other games' animate() functions can position/rotate freely. */
      .mpt-phone{width:44px;height:82px;border-radius:10px;background:linear-gradient(160deg,#2b2f38,#12141a);
        border:2px solid #000;position:absolute;top:50%;left:50%;margin:-41px 0 0 -22px;
        box-shadow:0 6px 14px rgba(0,0,0,.3)}
      .mpt-phone::before{content:'';position:absolute;top:6px;left:7px;right:7px;bottom:6px;border-radius:4px;
        background:linear-gradient(160deg,#bfe3ff,#7fc7ff)}
      .mpt-phone::after{content:'';position:absolute;top:3px;left:50%;transform:translateX(-50%);width:13px;
        height:3px;border-radius:2px;background:#000}
    `;
    document.head.appendChild(style);
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
  }

  // Minimal **bold** support so a step can call out the one word that matters
  // ("the instant it turns **green**") without a full markdown dependency.
  function boldify(s) {
    return s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  }

  function show(gameId, opts) {
    opts = opts || {};
    const info = window.MP_TUTORIALS && window.MP_TUTORIALS[gameId];
    if (!info) return null;
    ensureStyle();

    const overlay = document.createElement('div');
    overlay.className = 'mpt-overlay';
    overlay.innerHTML = `
      <div class="mpt-card" role="dialog" aria-label="How to play ${escapeHtml(info.title || '')}">
        <div class="mpt-head">
          <span class="mpt-emoji">${info.emoji || ''}</span>
          <span class="mpt-title">${escapeHtml(info.title || '')}</span>
          <button class="mpt-close" aria-label="Close">✕</button>
        </div>
        <div class="mpt-stage" id="mpt-stage"></div>
        <ol class="mpt-steps">${(info.steps || []).map((s) => `<li>${boldify(escapeHtml(s))}</li>`).join('')}</ol>
        <button class="btn primary mpt-gotit">Got it, let's play!</button>
      </div>
    `;
    document.body.appendChild(overlay);

    let stopAnim = null;
    if (typeof info.animate === 'function') {
      try {
        stopAnim = info.animate(overlay.querySelector('#mpt-stage'));
      } catch (e) {
        stopAnim = null;
      }
    }

    function close() {
      if (typeof stopAnim === 'function') stopAnim();
      overlay.remove();
      if (opts.onClose) opts.onClose();
    }
    overlay.querySelector('.mpt-close').addEventListener('click', close);
    overlay.querySelector('.mpt-gotit').addEventListener('click', close);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) close(); });

    return { close };
  }

  window.MP_TUTORIALS = window.MP_TUTORIALS || {};
  window.MP_showTutorial = show;
})();
