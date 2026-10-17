// Docs-only companion to choice.js. The landing picker owns the shared choice;
// this file reads its storage shape without changing the picker script.
(() => {
  "use strict";

  const KEY = "homebot.choice";
  const PERSONAS = ["neatpik", "rosie", "wall-e", "marvin", "r2-d2", "johnny-5", "hal-9000", "miss-minute"];
  const VALID = /^[a-z][a-z0-9-]{1,31}$/;
  let reserved = new Set();

  function read() {
    try {
      const value = JSON.parse(localStorage.getItem(KEY) || "null");
      if (!value || typeof value !== "object") return { persona: "neatpik", names: {} };
      const persona = PERSONAS.includes(value.persona) ? value.persona : "neatpik";
      const names = {};
      for (const id of PERSONAS) {
        const name = value.names && value.names[id];
        if (typeof name === "string" && VALID.test(name) && !reserved.has(name)) names[id] = name;
      }
      if (!value.names && typeof value.name === "string" && VALID.test(value.name) && !reserved.has(value.name)) {
        names[persona] = value.name; // landing's previous storage shape
      }
      return { persona, names };
    } catch {
      return { persona: "neatpik", names: {} };
    }
  }

  function save(value) {
    try { localStorage.setItem(KEY, JSON.stringify(value)); } catch { /* blocked storage */ }
  }

  let choice = read();
  let controls;

  function currentName() {
    const name = choice.names[choice.persona] || "";
    return VALID.test(name) && !reserved.has(name) ? name : "";
  }

  function swap() {
    const name = currentName();
    for (const command of document.querySelectorAll(".hb-cmd")) command.textContent = name || "homebot";
    const content = document.querySelector(".main-content");
    if (!content) return;
    let aside = content.querySelector(".hb-also");
    if (!name || !content.querySelector(".hb-cmd")) {
      aside?.remove();
      return;
    }
    if (!aside) {
      aside = document.createElement("p");
      aside.className = "hb-also";
      aside.innerHTML = "<code>homebot</code> works too.";
      content.prepend(aside);
    }
  }

  function render() {
    if (controls) {
      controls.select.value = choice.persona;
      controls.name.value = currentName();
    }
    swap();
  }

  function mount() {
    const right = document.querySelector(".docmd-menubar .menubar-right");
    if (right && !right.querySelector(".hb-choice")) {
      const wrap = document.createElement("div");
      wrap.className = "hb-choice";
      const label = document.createElement("label");
      label.textContent = "Robot ";
      const select = document.createElement("select");
      select.setAttribute("aria-label", "Robot");
      for (const id of PERSONAS) select.add(new Option(id, id));
      label.append(select);
      const nameLabel = document.createElement("label");
      nameLabel.textContent = "Name ";
      const name = document.createElement("input");
      name.type = "text";
      name.placeholder = "homebot";
      name.maxLength = 32;
      name.autocapitalize = "none";
      name.autocomplete = "off";
      name.spellcheck = false;
      nameLabel.append(name);
      const message = document.createElement("span");
      message.className = "hb-choice-error";
      message.setAttribute("role", "status");
      wrap.append(label, nameLabel, message);
      right.prepend(wrap);
      controls = { select, name, message };
      select.addEventListener("change", () => {
        choice.persona = select.value;
        save(choice);
        render();
      });
      name.addEventListener("input", () => {
        const candidate = name.value.trim();
        const good = candidate === "" || candidate === "homebot" || (VALID.test(candidate) && !reserved.has(candidate));
        name.setAttribute("aria-invalid", String(!good));
        message.textContent = good ? "" : "Use 2–32 lowercase letters, digits or hyphens; the name must be available.";
        if (!good) return;
        if (candidate === "" || candidate === "homebot") delete choice.names[choice.persona];
        else choice.names[choice.persona] = candidate;
        save(choice);
        swap();
      });
    } else if (right && !controls) {
      const wrap = right.querySelector(".hb-choice");
      if (wrap) controls = { select: wrap.querySelector("select"), name: wrap.querySelector("input"), message: wrap.querySelector(".hb-choice-error") };
    }
    render();
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount, { once: true });
  else mount();
  document.addEventListener("docmd:page-mounted", mount);
  fetch("/docs/assets/js/reserved-names.json")
    .then((response) => response.ok ? response.json() : Promise.reject(new Error(String(response.status))))
    .then((data) => {
      reserved = new Set([...(data.homebot || []), ...(data.shell || []), ...(data.tools || [])]);
      choice = read();
      render();
    })
    .catch(() => { /* The machine checks availability again during setup. */ });
})();
