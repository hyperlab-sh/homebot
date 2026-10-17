// The homepage picker: pick a robot, name it, and the install line, the
// terminal and every command on the page follow the choice.
//
// The robots are a slider: the picked robot's card is in front (data-pos 0)
// and its neighbours sit behind it (-1 and 1; site.css draws the rest). The
// radios still carry the choice, so the arrow keys, a click on a side card,
// the previous and next buttons and a swipe all just pick a robot. The name
// field moves to the bottom of the card in front; every other card shows a
// copy of it (inert, no ids, hidden from screen readers) with that robot's
// remembered name, and a click on the copy picks the card like a click
// anywhere else on it.
//
// Without this script the robots are a row of plain cards (their radios stay
// disabled) and the page shows the plain install line and `homebot`
// everywhere. The picked robot goes on <html data-persona>, so CSS can follow
// it (the bot's icon, --bot-icon in site.css). The choice is kept in localStorage
// ("homebot.choice", {"persona", "names": {"<persona>": "<name>"}}) so it
// survives a reload; each robot remembers its own name, and switching robots
// brings that name back. The old shape {"persona", "name"} is still read.
// Storage that is blocked or full only means nothing is kept.
//
// "Suggest a name" fills the field with a random name from the robot's list
// (SUGGESTIONS), never the one already there, then renders as if typed.
//
// The name check is a preview: the syntax rule is config.ValidName's, and the
// reserved words come from reserved-names.json (hand-written until the naming
// plan's N4 generates it). `homebot setup` checks the name again on the
// machine, where it can also see PATH.
(() => {
  "use strict";

  const KEY = "homebot.choice";
  const INSTALL = "curl -fsSL https://homebot.sh/install | bash";
  const SYNTAX = /^[a-z][a-z0-9-]{1,31}$/;

  const pick = document.getElementById("pick");
  const nameInput = document.getElementById("pick-name");
  const msg = document.getElementById("pick-msg");
  const installCmd = document.getElementById("install-cmd");
  if (!pick || !nameInput || !msg || !installCmd) return;

  const radios = [...pick.querySelectorAll('input[name="persona"]')];
  const ids = radios.map((r) => r.value);
  const cards = radios.map((r) => r.closest("li"));
  const nameBox = nameInput.closest(".pick-name");

  /** Suggested names per robot. Each is valid, not reserved and not a common command. */
  const SUGGESTIONS = {
    neatpik: [
      "pik", "nib", "nit", "nitpik", "snug", "tuck", "dusty", "ordo", "tidbit", "mop", "pixie",
      "nook", "shelf", "neatnik", "spruce", "crisp", "pinch", "trim", "tally", "sorted",
    ],
    rosie: [
      "rosie", "ros", "rosa", "mabel", "edna", "hattie", "dolly", "ethel", "flo", "mavis", "peggy",
      "gladys", "trudy", "mildred", "bess", "nellie", "opal", "pearl", "maude", "agnes",
    ],
    "wall-e": [
      "wall-e", "walle", "wally", "scrap", "junk", "bolt", "cube", "sprout", "spark", "scrapper",
      "stack", "pile", "rust", "cog", "tread", "compact", "salvage", "tinker",
    ],
    marvin: [
      "marvin", "marv", "gloom", "sigh", "woe", "ennui", "doom", "dread", "void", "mope", "glum",
      "bleak", "moody", "droop", "dismal", "nihil", "blah", "meh",
    ],
    "r2-d2": [
      "r2-d2", "r2", "r2d2", "r5", "r3", "bb", "g4", "q5", "k2", "x1", "u4", "m3", "t7", "ax-1",
      "zx-9", "unit-7", "e9", "p4",
    ],
    "johnny-5": [
      "johnny-5", "johnny5", "j5", "no5", "number5", "zap", "jolt", "zippy", "buzz", "sprint",
      "wired", "nova", "lively", "spunky", "alive", "zing", "turbo", "hop",
    ],
    "hal-9000": [
      "hal", "hal9000", "hal-9k", "calm", "logic", "serene", "poise", "steady", "lucid", "nominal",
      "cortex", "orbit", "ember", "pod-bay", "mission", "quiet", "stable", "aloof",
    ],
    "miss-minute": [
      "miss-minute", "minute", "minnie", "tick", "tock", "tickle", "hourglass", "tickety",
      "sundial", "chime", "peach", "magnolia", "sugar", "honey", "darlin", "pendulum", "belle",
      "dixie",
    ],
  };

  /** Reserved words by why they are taken; filled once the JSON arrives. */
  let reserved = { homebot: new Set(), shell: new Set(), tools: new Set() };
  /** The last name that passed, shown while the field holds one that doesn't. */
  let lastGood = "";
  /** The last good name per robot ({persona: name}); only real names are kept. */
  let names = {};
  /** Each card's copy of the name field, by robot; made once the script runs. */
  const copies = new Map();

  // ------------------------------------------------------------- storage

  function load() {
    try {
      const o = JSON.parse(localStorage.getItem(KEY) || "null");
      if (!o || typeof o !== "object") return null;
      const persona = ids.includes(o.persona) ? o.persona : null;
      const good = (v) => typeof v === "string" && SYNTAX.test(v);
      const kept = {};
      if (o.names && typeof o.names === "object" && !Array.isArray(o.names)) {
        for (const id of ids) if (good(o.names[id])) kept[id] = o.names[id];
      } else if (persona && good(o.name)) {
        kept[persona] = o.name; // the old shape: one name, for the picked robot
      }
      return { persona, names: kept };
    } catch {
      return null;
    }
  }

  function save(choice) {
    try {
      localStorage.setItem(KEY, JSON.stringify(choice));
    } catch {
      // Private mode or blocked storage: the choice lives for this page only.
    }
  }

  // ---------------------------------------------------------------- rules

  /** {ok, name} for a usable name ("" means none), or {ok: false, why}. */
  function check(raw) {
    const v = raw.trim();
    if (v === "" || v === "homebot") return { ok: true, name: "" };
    if (/^[A-Za-z][A-Za-z0-9-]*$/.test(v) && /[A-Z]/.test(v)) {
      return { ok: false, why: `Lowercase only: try ${v.toLowerCase()}.` };
    }
    if (!/^[a-z]/.test(v)) return { ok: false, why: "Start with a lowercase letter." };
    if (!/^[a-z][a-z0-9-]*$/.test(v)) return { ok: false, why: "Use only lowercase letters, digits and -." };
    if (v.length < 2) return { ok: false, why: "At least 2 characters." };
    if (v.length > 32) return { ok: false, why: "At most 32 characters." };
    if (reserved.homebot.has(v)) return { ok: false, why: `homebot uses “${v}” itself. Pick another name.` };
    if (reserved.shell.has(v)) return { ok: false, why: `Your shell uses “${v}”. Pick another name.` };
    if (reserved.tools.has(v)) {
      return { ok: false, why: `homebot runs a program called ${v}, and would end up running itself. Pick another name.` };
    }
    return { ok: true, name: v };
  }

  // --------------------------------------------------------------- render

  function persona() {
    const r = radios.find((x) => x.checked);
    return r ? r.value : "neatpik";
  }

  /** Put the picked robot's card in front and the name field at its bottom. */
  function layout(p) {
    const n = cards.length;
    const front = ids.indexOf(p);
    cards.forEach((card, i) => {
      let d = (i - front + n) % n; // steps to the right, going round
      if (d > n / 2) d -= n;
      card.dataset.pos = String(Math.max(-2, Math.min(2, d)));
    });
    if (nameBox && nameBox.parentElement !== cards[front]) cards[front].append(nameBox);
    for (const [id, copy] of copies) copy.hidden = id === p;
  }

  /** Give every card a copy of the name field, for when it is not in front. */
  function makeCopies() {
    if (!nameBox) return;
    cards.forEach((card, i) => {
      const copy = nameBox.cloneNode(true);
      for (const el of [copy, ...copy.querySelectorAll("*")]) {
        for (const a of ["id", "for", "aria-describedby", "aria-live"]) el.removeAttribute(a);
      }
      copy.classList.add("pick-name-copy");
      copy.inert = true;
      copy.setAttribute("aria-hidden", "true");
      const m = copy.querySelector(".pick-msg");
      if (m) m.textContent = "";
      card.append(copy);
      copies.set(ids[i], copy);
    });
  }

  function render() {
    const p = persona();
    layout(p);
    const verdict = check(nameInput.value);
    if (verdict.ok) {
      lastGood = verdict.name;
      nameInput.removeAttribute("aria-invalid");
      msg.textContent = "";
    } else {
      nameInput.setAttribute("aria-invalid", "true");
      msg.textContent = verdict.why;
      // A kept name can turn out reserved once the list arrives.
      if (!check(lastGood).ok) lastGood = "";
    }

    const name = lastGood;
    installCmd.textContent = `${INSTALL} -s -- --persona ${p}${name ? ` --name ${name}` : ""}`;
    for (const el of document.querySelectorAll(".hb-cmd")) el.textContent = name || "homebot";
    for (const el of document.querySelectorAll(".hb-name")) el.textContent = name || el.dataset.default;

    const also = document.getElementById("also");
    if (also) also.hidden = !name;
    for (const t of document.querySelectorAll("[data-voice]")) t.dataset.voice = p;
    document.documentElement.dataset.persona = p;

    if (name) names[p] = name;
    else delete names[p];
    save({ persona: p, names });
    for (const [id, copy] of copies) {
      const field = copy.querySelector("input");
      if (field) field.value = names[id] || "";
    }
  }

  /** Show the picked robot's remembered name (or nothing) and render. */
  function switchPersona() {
    lastGood = names[persona()] || "";
    nameInput.value = lastGood;
    render();
  }

  /** Pick the robot k cards to the right (k < 0 goes left), going round. */
  function step(k) {
    const n = radios.length;
    radios[(ids.indexOf(persona()) + k + n) % n].checked = true;
    switchPersona();
  }

  /** Fill the field with a random suggestion for this robot, then render. */
  function suggest() {
    const all = SUGGESTIONS[persona()] || [];
    const here = nameInput.value.trim();
    const pool = all.filter((n) => n !== here && check(n).ok);
    const list = pool.length ? pool : all;
    if (!list.length) return;
    nameInput.value = list[Math.floor(Math.random() * list.length)];
    render();
  }

  // ----------------------------------------------------------------- wire

  const saved = load();
  if (saved) {
    if (saved.persona) for (const r of radios) r.checked = r.value === saved.persona;
    names = saved.names;
  }
  lastGood = names[persona()] || "";
  nameInput.value = lastGood;

  for (const r of radios) r.disabled = false;
  for (const el of document.querySelectorAll(".js-only[hidden]")) el.hidden = false;
  makeCopies();
  pick.classList.add("is-live");

  pick.addEventListener("change", (e) => {
    if (e.target.name === "persona") switchPersona();
  });
  nameInput.addEventListener("input", render);
  const suggestBtn = document.getElementById("pick-suggest");
  if (suggestBtn) suggestBtn.addEventListener("click", suggest);
  pick.querySelector(".stage-prev")?.addEventListener("click", () => step(-1));
  pick.querySelector(".stage-next")?.addEventListener("click", () => step(1));

  // A sideways swipe over the cards picks the next robot. The click that can
  // follow it is dropped, so a swipe that starts on a side card doesn't also
  // pick that card. The name field is left alone: dragging there selects text.
  const list = cards[0]?.parentElement;
  let from = null;
  let swiped = false;
  list?.addEventListener("pointerdown", (e) => {
    swiped = false;
    from = e.isPrimary && !e.target.closest(".pick-name") ? { x: e.clientX, y: e.clientY } : null;
  });
  list?.addEventListener("pointercancel", () => (from = null));
  list?.addEventListener("dragstart", (e) => e.preventDefault()); // a mouse swipe would drag the picture
  list?.addEventListener("pointerup", (e) => {
    if (!from) return;
    const dx = e.clientX - from.x;
    const dy = e.clientY - from.y;
    from = null;
    if (Math.abs(dx) < 40 || Math.abs(dx) < Math.abs(dy)) return;
    swiped = true;
    step(dx < 0 ? 1 : -1);
  });
  list?.addEventListener("click", (e) => {
    if (!swiped) return;
    swiped = false;
    e.preventDefault();
    e.stopPropagation();
  }, true);
  // A click on a side card outside its label (the copy of the name field
  // on a phone, where it sits under the label) picks it too.
  list?.addEventListener("click", (e) => {
    const card = e.target instanceof Element ? e.target.closest(".robots > li") : null;
    if (!card || card.dataset.pos === "0" || e.target.closest("label")) return;
    const r = radios[cards.indexOf(card)];
    if (!r) return;
    r.checked = true;
    switchPersona();
  });

  render();
  // Cards slide from now on; the first layout lands without moving.
  requestAnimationFrame(() => requestAnimationFrame(() => pick.classList.add("is-ready")));

  fetch("/assets/js/reserved-names.json")
    .then((res) => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
    .then((data) => {
      const list = (k) => new Set(Array.isArray(data[k]) ? data[k].filter((x) => typeof x === "string") : []);
      reserved = { homebot: list("homebot"), shell: list("shell"), tools: list("tools") };
      // A remembered name can turn out reserved: forget it.
      for (const id of Object.keys(names)) if (!check(names[id]).ok) delete names[id];
      render();
    })
    .catch(() => {
      // Only the syntax is checked then; setup checks the rest.
    });
})();
