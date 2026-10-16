// Nexus Play: the little client-side behavior pages need beyond htmx.
(function () {
  "use strict";

  var modal = document.getElementById("modal");
  var modalBody = document.getElementById("modal-body");

  function openModal() {
    if (modal && !modal.open) modal.showModal();
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
