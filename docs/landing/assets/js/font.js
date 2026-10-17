// The reading font: a switch (a button with role="switch") in the top bar, on
// the homepage and in the docs, that sets every sans-serif text in OpenDyslexic.
//
// The choice goes on <html data-font="dyslexic">, where site.css and the docs'
// homebot.css swap their sans-serif font, and in localStorage ("homebot.font"),
// which the homepage and the docs share (same origin). A line in every page's
// <head> also sets the attribute, before the first paint: in index.html for the
// homepage, added to the docs' pages by assemble.ts.
//
// The homepage has the switch in its HTML (hidden until this script runs). In
// the docs this script adds it to docmd's menubar, and adds it again after
// every page change, because docmd's in-page navigation replaces the
// menubar's contents. assemble.ts copies this file to docs/assets/js/.
// Storage that is blocked only means the choice lasts for this page.
(() => {
  "use strict";

  const KEY = "homebot.font";
  const root = document.documentElement;

  function stored() {
    try {
      return localStorage.getItem(KEY) === "dyslexic";
    } catch {
      return false;
    }
  }

  function save(on) {
    try {
      if (on) localStorage.setItem(KEY, "dyslexic");
      else localStorage.removeItem(KEY);
    } catch {
      // Private mode or blocked storage: nothing is kept.
    }
  }

  function apply(on) {
    if (on) root.dataset.font = "dyslexic";
    else delete root.dataset.font;
    for (const b of document.querySelectorAll(".font-toggle")) b.setAttribute("aria-checked", String(on));
  }

  /** The switch for docmd's menubar, shaped like the homepage's. */
  function docsButton() {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "font-toggle menubar-link";
    b.setAttribute("role", "switch");
    b.setAttribute("aria-checked", "false");
    b.innerHTML =
      '<span class="font-toggle-switch" aria-hidden="true"></span><span class="font-toggle-text">Dyslexia font</span>';
    return b;
  }

  function mountDocs() {
    const bar = document.querySelector(".docmd-menubar .menubar-right");
    if (bar && !bar.querySelector(".font-toggle")) bar.prepend(docsButton());
  }

  document.addEventListener("click", (e) => {
    const b = e.target instanceof Element && e.target.closest(".font-toggle");
    if (!b) return;
    const on = b.getAttribute("aria-checked") !== "true";
    save(on);
    apply(on);
  });
  // Another tab flipped it.
  window.addEventListener("storage", (e) => {
    if (e.key === KEY) apply(stored());
  });
  document.addEventListener("docmd:page-mounted", () => {
    mountDocs();
    apply(stored());
  });

  mountDocs();
  for (const b of document.querySelectorAll(".font-toggle[hidden]")) b.hidden = false;
  apply(stored());
})();
