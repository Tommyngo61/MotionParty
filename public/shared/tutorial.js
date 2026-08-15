// Shared "how to play" demo registry. Each game registers itself into
// window.MP_TUTORIALS (same pattern as MP_GAMES / MP_CONTROLLERS) with an
// emoji, title, and a render(stageEl) function that fills stageEl with a demo
// of the actual motion and returns a stop() cleanup function. The host's
// player-select screen renders this inline and auto-plays it the moment that
// screen opens - no button, no modal.
//
// Registration shape:
//   window.MP_TUTORIALS.<gameId> = {
//     emoji: '🎾', title: 'Motion Tennis',
//     render(stageEl) { ...; return () => { /* stop/cleanup */ }; },
//   };
(function () {
  const STYLE_ID = 'mpt-style';
  function ensureStyle() {
    if (document.getElementById(STYLE_ID)) return;
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      .mpt-stage{position:relative;background:#000;border-radius:16px;overflow:hidden;width:100%;max-width:420px;
        aspect-ratio:16/10;margin:0 auto}
      .mpt-stage video{width:100%;height:100%;object-fit:cover;display:block}
      /* Fallback CSS demo (used only for games without captured footage yet). */
      .mpt-stage.mpt-css-demo{background:radial-gradient(circle at 50% 20%,#eaf6ff,#dcefff 70%)}
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

  // Renders gameId's tutorial into stageEl (clearing whatever was there) and
  // returns a stop() to call before the next render or when leaving the screen.
  function render(stageEl, gameId) {
    ensureStyle();
    stageEl.innerHTML = '';
    stageEl.className = 'mpt-stage';
    const info = window.MP_TUTORIALS && window.MP_TUTORIALS[gameId];
    if (!info || typeof info.render !== 'function') {
      stageEl.style.display = 'none';
      return () => {};
    }
    stageEl.style.display = '';
    let stop = null;
    try {
      stop = info.render(stageEl);
    } catch (e) {
      stop = null;
    }
    return typeof stop === 'function' ? stop : () => {};
  }

  window.MP_TUTORIALS = window.MP_TUTORIALS || {};
  window.MP_renderTutorial = render;
})();
