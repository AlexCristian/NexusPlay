// Nexus Play: the little client-side behavior pages need beyond htmx.
(function () {
  "use strict";

  var modal = document.getElementById("modal");
  var modalBody = document.getElementById("modal-body");

  // The dialog itself takes focus, not its first button: a focused close
  // button shows a focus ring as soon as the dialog opens.
  function openModal() {
    if (modal && !modal.open) {
      modal.showModal();
      modal.focus();
    }
  }

  // Anything htmx swaps into the modal body opens the modal.
  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail.target === modalBody) openModal();
  });

  if (modal) {
    modal.addEventListener("click", function (e) {
      // A click on the backdrop (outside the dialog box) closes it.
      if (e.target === modal) modal.close();
      if (e.target.closest("[data-close-modal]")) modal.close();
    });
    modal.addEventListener("close", function () {
      modalBody.innerHTML = "";
    });
  }

  // Search drawer. htmx puts results in #search-results as the visitor types;
  // an empty response (no query, or nothing found) closes the drawer. Opening
  // slides it down (CSS) and dims the page under it.
  var drawer = document.getElementById("search-drawer");
  var results = document.getElementById("search-results");
  var cover = document.getElementById("page-cover");

  function setDrawer(open) {
    if (!drawer) return;
    drawer.classList.toggle("open", open);
    cover.classList.toggle("dimmed", open);
  }

  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail.target === results) setDrawer(results.children.length > 0);
  });

  // Escape in the search box clears it and closes the drawer.
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape" || !e.target.matches(".search input")) return;
    e.target.value = "";
    results.replaceChildren();
    setDrawer(false);
  });

  // Tabs: a [data-tabs] container with [data-tab] buttons naming panel ids.
  document.addEventListener("click", function (e) {
    var tab = e.target.closest("[data-tab]");
    if (!tab) return;
    var group = tab.closest("[data-tabs]");
    var scope = group.dataset.tabsScope ? group.closest(group.dataset.tabsScope) : group;
    group.querySelectorAll("[data-tab]").forEach(function (t) {
      var selected = t === tab;
      t.setAttribute("aria-selected", String(selected));
      var panel = scope.querySelector("#" + CSS.escape(t.dataset.tab));
      if (panel) panel.hidden = !selected;
    });
    // A long row of seasons scrolls sideways; the chosen one moves to the
    // middle, so the seasons next to it come into view (as in the original).
    if (group.scrollWidth > group.clientWidth) {
      var left = tab.offsetLeft - group.offsetLeft - (group.clientWidth - tab.offsetWidth) / 2;
      group.scrollTo({ left: Math.max(0, left), behavior: "smooth" });
    }
  });

  // Forms with data-confirm ask before submitting.
  document.addEventListener("submit", function (e) {
    var msg = e.target.dataset && e.target.dataset.confirm;
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  // Errors from htmx requests: show a readable message instead of failing silently.
  document.addEventListener("htmx:responseError", function (e) {
    var xhr = e.detail.xhr;
    if (xhr.status === 401) return; // handled by an HX-Redirect to the login page
    // HTML error responses are swapped in by htmx (see responseHandling in the layout).
    if (/^\s*</.test(xhr.responseText || "")) return;
    var target = e.detail.target;
    if (!target) return;
    var div = document.createElement("div");
    div.className = "alert alert-error";
    div.setAttribute("role", "status");
    div.textContent = (xhr.responseText && xhr.responseText.length < 300)
      ? xhr.responseText
      : "Something went wrong. Please try again.";
    if (target === modalBody) {
      modalBody.replaceChildren(div);
      openModal();
    } else {
      target.prepend(div);
    }
  });
})();
