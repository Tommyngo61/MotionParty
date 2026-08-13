// Original hand-coded vector avatars (simple flat shapes), not AI-generated images.
// Every avatar is a self-contained 100x100 viewBox SVG string.
(function (global) {
  function svg(inner) {
    return `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">${inner}</svg>`;
  }

  // Reusable eye/nose helpers keep the markup short and consistent.
  const EYES = '<circle cx="38" cy="52" r="4.5" fill="#20242b"/><circle cx="62" cy="52" r="4.5" fill="#20242b"/>' +
    '<circle cx="39.5" cy="50.5" r="1.3" fill="#fff"/><circle cx="63.5" cy="50.5" r="1.3" fill="#fff"/>';

  const AVATARS = [
    {
      id: 'cat',
      name: 'Cat',
      color: '#f2994a',
      svg: svg(`
        <circle cx="50" cy="55" r="34" fill="#f2994a"/>
        <path d="M22 34 L32 12 L44 32 Z" fill="#f2994a"/>
        <path d="M78 34 L68 12 L56 32 Z" fill="#f2994a"/>
        <path d="M27 32 L33 20 L40 30 Z" fill="#ffd7a8"/>
        <path d="M73 32 L67 20 L60 30 Z" fill="#ffd7a8"/>
        <ellipse cx="50" cy="62" rx="16" ry="12" fill="#fff3e6"/>
        ${EYES}
        <path d="M46 63 L54 63 L50 68 Z" fill="#20242b"/>
        <path d="M8 58 Q26 54 40 62" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
        <path d="M8 68 Q26 66 40 66" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
        <path d="M92 58 Q74 54 60 62" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
        <path d="M92 68 Q74 66 60 66" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
      `),
    },
    {
      id: 'dog',
      name: 'Dog',
      color: '#c88b52',
      svg: svg(`
        <circle cx="50" cy="55" r="34" fill="#d9a066"/>
        <ellipse cx="20" cy="46" rx="12" ry="20" fill="#a9702f" transform="rotate(-15 20 46)"/>
        <ellipse cx="80" cy="46" rx="12" ry="20" fill="#a9702f" transform="rotate(15 80 46)"/>
        <ellipse cx="50" cy="66" rx="20" ry="14" fill="#f4e3cf"/>
        ${EYES}
        <ellipse cx="50" cy="66" rx="7" ry="5.5" fill="#20242b"/>
        <path d="M50 71 Q50 78 42 78" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
      `),
    },
    {
      id: 'fox',
      name: 'Fox',
      color: '#e8622c',
      svg: svg(`
        <circle cx="50" cy="57" r="33" fill="#e8622c"/>
        <path d="M20 36 L30 10 L42 32 Z" fill="#e8622c"/>
        <path d="M80 36 L70 10 L58 32 Z" fill="#e8622c"/>
        <path d="M24 32 L30 18 L37 30 Z" fill="#fff"/>
        <path d="M76 32 L70 18 L63 30 Z" fill="#fff"/>
        <path d="M50 55 L34 72 Q50 82 66 72 Z" fill="#fff"/>
        ${EYES}
        <path d="M46 66 L54 66 L50 71 Z" fill="#20242b"/>
      `),
    },
    {
      id: 'bear',
      name: 'Bear',
      color: '#8a5a3b',
      svg: svg(`
        <circle cx="22" cy="26" r="11" fill="#8a5a3b"/>
        <circle cx="78" cy="26" r="11" fill="#8a5a3b"/>
        <circle cx="22" cy="26" r="5" fill="#c99a72"/>
        <circle cx="78" cy="26" r="5" fill="#c99a72"/>
        <circle cx="50" cy="56" r="34" fill="#8a5a3b"/>
        <ellipse cx="50" cy="64" rx="18" ry="14" fill="#c99a72"/>
        ${EYES}
        <ellipse cx="50" cy="63" rx="7" ry="5.5" fill="#20242b"/>
        <path d="M50 68 Q50 74 43 74" stroke="#20242b" stroke-width="2" fill="none" stroke-linecap="round"/>
      `),
    },
    {
      id: 'rabbit',
      name: 'Rabbit',
      color: '#e8b4c8',
      svg: svg(`
        <ellipse cx="36" cy="22" rx="9" ry="24" fill="#e8b4c8"/>
        <ellipse cx="64" cy="22" rx="9" ry="24" fill="#e8b4c8"/>
        <ellipse cx="36" cy="24" rx="4" ry="16" fill="#fff0f5"/>
        <ellipse cx="64" cy="24" rx="4" ry="16" fill="#fff0f5"/>
        <circle cx="50" cy="60" r="30" fill="#fbe4ee"/>
        ${EYES}
        <ellipse cx="50" cy="66" rx="4" ry="3" fill="#e07a9a"/>
        <path d="M20 62 Q34 58 44 64" stroke="#20242b" stroke-width="1.6" fill="none" stroke-linecap="round"/>
        <path d="M80 62 Q66 58 56 64" stroke="#20242b" stroke-width="1.6" fill="none" stroke-linecap="round"/>
      `),
    },
    {
      id: 'panda',
      name: 'Panda',
      color: '#2b2b2b',
      svg: svg(`
        <circle cx="21" cy="24" r="12" fill="#20242b"/>
        <circle cx="79" cy="24" r="12" fill="#20242b"/>
        <circle cx="50" cy="56" r="34" fill="#fdfdfd"/>
        <ellipse cx="33" cy="52" rx="11" ry="14" fill="#20242b" transform="rotate(-10 33 52)"/>
        <ellipse cx="67" cy="52" rx="11" ry="14" fill="#20242b" transform="rotate(10 67 52)"/>
        <circle cx="35" cy="54" r="4" fill="#fff"/>
        <circle cx="65" cy="54" r="4" fill="#fff"/>
        <circle cx="35" cy="54" r="1.6" fill="#20242b"/>
        <circle cx="65" cy="54" r="1.6" fill="#20242b"/>
        <ellipse cx="50" cy="68" rx="6" ry="4.5" fill="#20242b"/>
      `),
    },
    {
      id: 'tiger',
      name: 'Tiger',
      color: '#f2a33c',
      svg: svg(`
        <circle cx="50" cy="55" r="34" fill="#f2a33c"/>
        <path d="M20 32 L30 10 L42 30 Z" fill="#f2a33c"/>
        <path d="M80 32 L70 10 L58 30 Z" fill="#f2a33c"/>
        <ellipse cx="50" cy="64" rx="18" ry="13" fill="#fff3e0"/>
        <path d="M14 40 L26 44 M16 50 L28 51 M86 40 L74 44 M84 50 L72 51" stroke="#3a2410" stroke-width="3" stroke-linecap="round"/>
        <path d="M40 20 L44 30 M60 20 L56 30 M50 16 L50 27" stroke="#3a2410" stroke-width="3" stroke-linecap="round"/>
        ${EYES}
        <path d="M46 65 L54 65 L50 70 Z" fill="#20242b"/>
      `),
    },
    {
      id: 'koala',
      name: 'Koala',
      color: '#9aa5ad',
      svg: svg(`
        <circle cx="18" cy="42" r="16" fill="#9aa5ad"/>
        <circle cx="82" cy="42" r="16" fill="#9aa5ad"/>
        <circle cx="18" cy="42" r="8" fill="#5b6670"/>
        <circle cx="82" cy="42" r="8" fill="#5b6670"/>
        <circle cx="50" cy="56" r="32" fill="#b9c2c8"/>
        <ellipse cx="50" cy="66" rx="12" ry="9" fill="#3a3f44"/>
        ${EYES}
      `),
    },
  ];

  global.MP_AVATARS = AVATARS;
  global.MP_getAvatar = function (id) {
    return AVATARS.find((a) => a.id === id) || AVATARS[0];
  };
})(window);
