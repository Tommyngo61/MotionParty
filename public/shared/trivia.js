// Original trivia content + board/face-off generation for Trivia Throwdown, shared
// by the host and both apps. A pool of categories/face-off questions is bundled
// here; the server picks a random 5 categories (+ a hidden Daily Double tile) and
// 5 face-off questions per match via a seed, the same MP_mulberry32 pattern
// tracks.js/mazes.js use, so every match plays a different board without the
// server having to ship the content itself.
// Works both as a browser <script> and as a plain Node require() (server/index.js
// needs the same board/face-off generation to stay authoritative), so it can't
// assume either module or window exists.
(function () {
  const mulberry32 = (typeof module !== 'undefined' && module.exports)
    ? require('./prng.js').MP_mulberry32
    : window.MP_mulberry32;

  const VALUES = [100, 200, 300, 400, 500];

  // Each category has exactly 5 clues, ordered easiest (100) to hardest (500).
  // Clues are written Jeopardy-style (a statement; the expected reply is phrased
  // as a question) purely as fun flavor for the host reading it aloud - nothing
  // here is copied from any actual quiz show.
  const CATEGORY_POOL = [
    { name: 'World Geography', clues: [
      { clue: "This is the largest country in the world by land area.", answer: "What is Russia?" },
      { clue: "This African river is the longest in the world.", answer: "What is the Nile?" },
      { clue: "This South American country is named for its position on the equator.", answer: "What is Ecuador?" },
      { clue: "This is the smallest country in the world, located entirely within the city of Rome.", answer: "What is Vatican City?" },
      { clue: "This landlocked southern African country is completely surrounded by a single other country.", answer: "What is Lesotho?" },
    ]},
    { name: 'Science & Nature', clues: [
      { clue: "This is the closest planet to the sun.", answer: "What is Mercury?" },
      { clue: "This gas makes up about 78% of Earth's atmosphere.", answer: "What is nitrogen?" },
      { clue: "This organelle is known as the powerhouse of the cell.", answer: "What is the mitochondria?" },
      { clue: "This scientist developed the theory of general relativity.", answer: "Who is Albert Einstein?" },
      { clue: "This is the only metal that's liquid at room temperature.", answer: "What is mercury?" },
    ]},
    { name: 'Movies', clues: [
      { clue: "This 1997 film about a doomed ocean liner starred Leonardo DiCaprio and Kate Winslet.", answer: "What is Titanic?" },
      { clue: "This wizarding-school franchise is based on books by J.K. Rowling.", answer: "What is Harry Potter?" },
      { clue: "This Pixar film follows a rat who dreams of becoming a Parisian chef.", answer: "What is Ratatouille?" },
      { clue: "This director's films include Jaws, E.T., and Jurassic Park.", answer: "Who is Steven Spielberg?" },
      { clue: "This 1994 film follows a man with a low IQ who unwittingly witnesses key moments in history.", answer: "What is Forrest Gump?" },
    ]},
    { name: 'Music', clues: [
      { clue: "This instrument has 88 keys.", answer: "What is the piano?" },
      { clue: "This British band released the album Abbey Road in 1969.", answer: "What is the Beatles?" },
      { clue: "This pop star is known as the 'Queen of Pop.'", answer: "Who is Madonna?" },
      { clue: "This composer was almost completely deaf by the time he wrote his Ninth Symphony.", answer: "Who is Beethoven?" },
      { clue: "This genre of music originated in New Orleans around the turn of the 20th century.", answer: "What is jazz?" },
    ]},
    { name: 'History', clues: [
      { clue: "This founding document begins with the words 'We the People.'", answer: "What is the U.S. Constitution?" },
      { clue: "This wall divided a European capital city from 1961 to 1989.", answer: "What is the Berlin Wall?" },
      { clue: "This Egyptian queen was famously allied with both Julius Caesar and Mark Antony.", answer: "Who is Cleopatra?" },
      { clue: "This global war lasted from 1939 to 1945.", answer: "What is World War II?" },
      { clue: "This ancient wonder was a lighthouse that once stood in the harbor of Alexandria, Egypt.", answer: "What is the Lighthouse of Alexandria?" },
    ]},
    { name: 'Sports', clues: [
      { clue: "This sport is played every summer at Wimbledon.", answer: "What is tennis?" },
      { clue: "This country has won the men's FIFA World Cup more times than any other.", answer: "What is Brazil?" },
      { clue: "This basketball legend was known as 'His Airness.'", answer: "Who is Michael Jordan?" },
      { clue: "The Summer Olympics are held once every this many years.", answer: "What is four?" },
      { clue: "This boxer famously declared 'I am the greatest' and changed his name after converting to Islam.", answer: "Who is Muhammad Ali?" },
    ]},
    { name: 'Food & Drink', clues: [
      { clue: "This Italian dish is a flat dough topped with sauce, cheese, and toppings.", answer: "What is pizza?" },
      { clue: "This drink is made by fermenting the juice of grapes.", answer: "What is wine?" },
      { clue: "This spicy fermented cabbage dish is a staple Korean side.", answer: "What is kimchi?" },
      { clue: "This French cooking technique means to cook something slowly, submerged in fat.", answer: "What is confit?" },
      { clue: "This fungus is what makes bread rise and beer ferment.", answer: "What is yeast?" },
    ]},
    { name: 'Animal Kingdom', clues: [
      { clue: "This is the largest land animal alive today.", answer: "What is the elephant?" },
      { clue: "This flightless bird is the fastest animal on two legs.", answer: "What is the ostrich?" },
      { clue: "This tusked marine mammal is nicknamed the 'unicorn of the sea.'", answer: "What is the narwhal?" },
      { clue: "This is the only mammal capable of true, sustained flight.", answer: "What is the bat?" },
      { clue: "This is the only continent where kangaroos are found in the wild.", answer: "What is Australia?" },
    ]},
  ];

  // Family-Feud-style survey prompts. `accept` lists synonyms/variants that all
  // count as that slot; `points` roughly mirrors a real Feud board (skewed toward
  // the most obvious answer) - see matchAnswer() below for how a free-text guess
  // gets resolved against these.
  const FACEOFF_POOL = [
    { prompt: "Name something you'd find in a kitchen.", answers: [
      { accept: ['refrigerator', 'fridge'], points: 35 },
      { accept: ['stove', 'oven', 'range'], points: 25 },
      { accept: ['sink'], points: 15 },
      { accept: ['microwave'], points: 13 },
      { accept: ['table'], points: 7 },
      { accept: ['knife', 'knives'], points: 5 },
    ]},
    { prompt: "Name a reason someone might be late to work.", answers: [
      { accept: ['traffic'], points: 40 },
      { accept: ['overslept', 'oversleeping', 'alarm'], points: 25 },
      { accept: ['car trouble', 'car broke down', 'flat tire'], points: 15 },
      { accept: ['kids', 'family', 'children'], points: 12 },
      { accept: ['weather', 'rain', 'snow'], points: 8 },
    ]},
    { prompt: "Name something you'd take to the beach.", answers: [
      { accept: ['towel'], points: 30 },
      { accept: ['sunscreen', 'sunblock'], points: 25 },
      { accept: ['umbrella'], points: 18 },
      { accept: ['swimsuit', 'bathing suit'], points: 15 },
      { accept: ['cooler', 'drinks'], points: 12 },
    ]},
    { prompt: "Name a popular pizza topping.", answers: [
      { accept: ['pepperoni'], points: 35 },
      { accept: ['cheese', 'extra cheese'], points: 25 },
      { accept: ['mushroom', 'mushrooms'], points: 15 },
      { accept: ['sausage'], points: 13 },
      { accept: ['olives', 'olive'], points: 7 },
      { accept: ['pineapple'], points: 5 },
    ]},
    { prompt: "Name something people do to relax.", answers: [
      { accept: ['sleep', 'nap'], points: 30 },
      { accept: ['watch tv', 'watch television', 'tv'], points: 25 },
      { accept: ['read', 'reading', 'book'], points: 18 },
      { accept: ['music', 'listen to music'], points: 15 },
      { accept: ['exercise', 'workout'], points: 12 },
    ]},
    { prompt: "Name an animal you might see at a zoo.", answers: [
      { accept: ['lion'], points: 30 },
      { accept: ['elephant'], points: 25 },
      { accept: ['monkey', 'monkeys'], points: 18 },
      { accept: ['giraffe'], points: 15 },
      { accept: ['tiger'], points: 12 },
    ]},
  ];

  function shuffled(arr, rand) {
    const a = arr.slice();
    for (let i = a.length - 1; i > 0; i--) {
      const j = Math.floor(rand() * (i + 1));
      [a[i], a[j]] = [a[j], a[i]];
    }
    return a;
  }

  // Picks 5 of the 8 pooled categories and a random hidden Daily Double tile.
  function generateBoard(seed) {
    const rand = mulberry32(seed >>> 0);
    const picked = shuffled(CATEGORY_POOL, rand).slice(0, 5);
    const categories = picked.map((c) => ({
      name: c.name,
      clues: c.clues.map((cl, i) => ({ clue: cl.clue, answer: cl.answer, value: VALUES[i] })),
    }));
    const dailyDouble = { cat: Math.floor(rand() * 5), val: Math.floor(rand() * 5) };
    return { categories, dailyDouble };
  }

  // Picks 5 of the 6 pooled face-off questions, in a random order.
  function generateFaceoff(seed) {
    const rand = mulberry32((seed + 0x9e3779b1) >>> 0);
    return shuffled(FACEOFF_POOL, rand).slice(0, 5);
  }

  // Normalizes a free-text guess and checks it against a face-off question's
  // answer slots - exact match, or either string containing the other, so "a
  // fridge" / "fridge" / "refrigerator" all land on the same slot.
  function matchAnswer(question, text) {
    const norm = String(text || '').toLowerCase().trim().replace(/[^a-z0-9 ]/g, '');
    if (!norm) return null;
    for (const slot of question.answers) {
      for (const term of slot.accept) {
        const t = term.toLowerCase();
        if (norm === t || norm.includes(t) || t.includes(norm)) return slot;
      }
    }
    return null;
  }

  const api = {
    MP_TRIVIA_VALUES: VALUES,
    MP_generateTriviaBoard: generateBoard,
    MP_generateTriviaFaceoff: generateFaceoff,
    MP_matchFaceoffAnswer: matchAnswer,
  };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (typeof window !== 'undefined') Object.assign(window, api);
})();
