// Keyboard access for the parts docmd draws for the mouse only.
//
// - Tabs: docmd renders them as plain divs that answer clicks. This gives them
//   the tab roles and keys (Tab reaches the active tab; arrows, Home and End
//   move between tabs) and leaves docmd's own click handler to switch panes.
// - Code blocks and tables that scroll sideways (on phones, mostly) get a tab
//   stop, so a keyboard can scroll them (WCAG 2.1.1). One that fits again,
//   after a resize or a font change, loses it.
//
// It runs on load and after each of docmd's in-page navigations
// (docmd:page-mounted), which replace the page's content.
(() => {
  "use strict";

  const SCROLLERS = ".main-content pre, .main-content .table-wrapper";
  let uid = 0;

  function syncTabs(nav) {
    for (const t of nav.querySelectorAll('[role="tab"]')) {
      const on = t.classList.contains("active");
      t.setAttribute("aria-selected", String(on));
      t.tabIndex = on ? 0 : -1;
    }
  }

  function setUpTabs() {
    for (const nav of document.querySelectorAll(".docmd-tabs-nav:not([role])")) {
      const box = nav.closest(".docmd-tabs");
      if (!box) continue;
      // the same panes docmd's click handler pairs with the tabs, by position
      const panes = box.querySelectorAll(".docmd-tab-pane");
      nav.setAttribute("role", "tablist");
      nav.querySelectorAll(".docmd-tabs-nav-item").forEach((t, i) => {
        t.id = `docmd-tab-${++uid}`;
        t.setAttribute("role", "tab");
        const pane = panes[i];
        if (!pane) return;
        pane.id = `${t.id}-panel`;
        pane.setAttribute("role", "tabpanel");
        pane.setAttribute("aria-labelledby", t.id);
        t.setAttribute("aria-controls", pane.id);
      });
      syncTabs(nav);
    }
  }

  function markScrollers() {
    for (const el of document.querySelectorAll(SCROLLERS)) {
      const scrolls = el.scrollWidth > el.clientWidth + 1;
      if (scrolls && !el.hasAttribute("tabindex")) {
        el.tabIndex = 0;
        el.dataset.scrollStop = "";
      } else if (!scrolls && "scrollStop" in el.dataset) {
        el.removeAttribute("tabindex");
        delete el.dataset.scrollStop;
      }
    }
  }

  function setUp() {
    setUpTabs();
    markScrollers();
  }

  document.addEventListener("keydown", (e) => {
    const tab = e.target instanceof Element ? e.target.closest('.docmd-tabs-nav [role="tab"]') : null;
    if (!tab) return;
    const tabs = [...tab.parentElement.querySelectorAll('[role="tab"]')];
    let i = tabs.indexOf(tab);
    if (e.key === "ArrowRight") i = (i + 1) % tabs.length;
    else if (e.key === "ArrowLeft") i = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === "Home") i = 0;
    else if (e.key === "End") i = tabs.length - 1;
    else if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    tabs[i].click(); // docmd's handler switches the classes before this returns
    syncTabs(tab.parentElement);
    tabs[i].focus();
  });

  let pending = 0;
  const later = () => {
    cancelAnimationFrame(pending);
    pending = requestAnimationFrame(markScrollers);
  };
  // A click on a tab: docmd's handler may run after this one, so mirror its
  // classes on the next turn; the pane it shows was measured while hidden.
  document.addEventListener("click", (e) => {
    const nav = e.target instanceof Element ? e.target.closest('.docmd-tabs-nav[role="tablist"]') : null;
    if (!nav) return;
    setTimeout(() => syncTabs(nav));
    later();
  });
  window.addEventListener("resize", later);
  document.fonts?.addEventListener("loadingdone", later);
  // the font switch (font.js) changes how wide every line is
  new MutationObserver(later).observe(document.documentElement, { attributes: true, attributeFilter: ["data-font"] });
  document.addEventListener("docmd:page-mounted", setUp);

  setUp();
})();
