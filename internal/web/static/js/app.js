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

  // The drawer closes on Escape anywhere, which also clears the query, and
  // on a click on the dimmed page under it (which is not clickable while
  // dimmed; see app.css).
  function closeSearch(clear) {
    if (clear) {
      var input = document.querySelector(".search input");
      if (input) input.value = "";
    }
    results.replaceChildren();
    setDrawer(false);
  }
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape" || (modal && modal.open)) return;
    if (drawer && (drawer.classList.contains("open") || e.target.matches(".search input"))) closeSearch(true);
  });
  if (cover) {
    cover.addEventListener("click", function (e) {
      if (!cover.classList.contains("dimmed")) return;
      e.preventDefault();
      closeSearch(false);
    });
  }

  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // A row of seasons that scrolls fades out at whichever edge has more
  // seasons behind it.
  function markEdges(row) {
    row.classList.toggle("more-start", row.scrollLeft > 2);
    row.classList.toggle("more-end", row.scrollLeft + row.clientWidth < row.scrollWidth - 2);
  }
  // A long description shows its expand button only when it is cut.
  function initClamps(root) {
    root.querySelectorAll(".clamp").forEach(function (c) {
      var text = c.querySelector(".overview"), button = c.querySelector(".clamp-toggle");
      var cut = text.scrollHeight > text.clientHeight + 2;
      c.classList.toggle("fits", !cut);
      button.hidden = !cut;
    });
  }
  function initContent(root) {
    root.querySelectorAll(".season-tabs").forEach(function (row) {
      markEdges(row);
      row.addEventListener("scroll", function () { markEdges(row); }, { passive: true });
    });
    initClamps(root);
  }
  document.addEventListener("click", function (e) {
    var b = e.target.closest(".clamp-toggle");
    if (!b) return;
    var c = b.closest(".clamp"), open = !c.classList.contains("open");
    c.classList.toggle("open", open);
    b.setAttribute("aria-expanded", String(open));
    b.setAttribute("aria-label", open ? "Show less of the description" : "Show the whole description");
    b.title = b.getAttribute("aria-label");
  });
  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail.target === modalBody) initContent(modalBody);
  });
  initContent(document);
  window.addEventListener("resize", function () { document.querySelectorAll(".season-tabs").forEach(markEdges); });

  // Popovers ("Previously watching", "My subscriptions") open under the
  // button that opened them, kept on screen, with the arrow pointing at it.
  document.querySelectorAll(".popover[popover]").forEach(function (pop) {
    pop.addEventListener("beforetoggle", function (e) {
      if (e.newState !== "open") return;
      var btn = document.querySelector('[popovertarget="' + pop.id + '"]');
      if (!btn) return;
      var r = btn.getBoundingClientRect();
      var width = Math.min(360, window.innerWidth - 16);
      var left = Math.max(8, Math.min(r.left + r.width / 2 - width / 2, window.innerWidth - width - 8));
      pop.style.width = width + "px";
      pop.style.left = left + "px";
      pop.style.top = (r.bottom + 8) + "px";
      pop.style.setProperty("--arrow-x", (r.left + r.width / 2 - left) + "px");
    });
  });

  // Tabs: a [data-tabs] container with [data-tab] buttons naming panel ids.
  document.addEventListener("click", function (e) {
    var tab = e.target.closest("[data-tab]");
    if (!tab) return;
    var group = tab.closest("[data-tabs]");
    var scope = group.dataset.tabsScope ? group.closest(group.dataset.tabsScope) : group;
    // Switching tabs fades the new panel in and eases the height between
    // the old panel and the new one, so the page below glides rather
    // than jumps.
    var shown = scope.querySelector("#" + CSS.escape(tab.dataset.tab));
    var box = shown && shown.parentElement;
    var before = box ? box.getBoundingClientRect().height : 0;
    group.querySelectorAll("[data-tab]").forEach(function (t) {
      var selected = t === tab;
      t.setAttribute("aria-selected", String(selected));
      var panel = scope.querySelector("#" + CSS.escape(t.dataset.tab));
      if (panel) panel.hidden = !selected;
    });
    if (box && !reduceMotion && box.animate) {
      var after = box.getBoundingClientRect().height;
      if (Math.abs(after - before) > 1) {
        box.style.overflow = "hidden";
        box.animate([{ height: before + "px" }, { height: after + "px" }], { duration: 250, easing: "ease-out" })
          .finished.then(function () { box.style.overflow = ""; }, function () {});
      }
      shown.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 200, easing: "ease-out" });
    }
    // A long row of seasons scrolls sideways; the chosen one moves to the
    // middle, so the seasons next to it come into view (as in the original).
    var row = tab.parentElement;
    if (row.scrollWidth > row.clientWidth) {
      var left = tab.offsetLeft - row.offsetLeft - (row.clientWidth - tab.offsetWidth) / 2;
      row.scrollTo({ left: Math.max(0, left), behavior: "smooth" });
    }
  });

  // Forms with data-confirm ask before submitting.
  document.addEventListener("submit", function (e) {
    var msg = e.target.dataset && e.target.dataset.confirm;
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  // A request that never reached the server (offline, server restarting):
  // say so where the answer would have gone, instead of doing nothing.
  function showFailure(target, msg) {
    if (!target) return;
    var div = document.createElement("div");
    div.className = "alert alert-error";
    div.setAttribute("role", "alert");
    div.textContent = msg;
    if (target === modalBody) {
      modalBody.replaceChildren(div);
      openModal();
    } else if (target === results) {
      var panel = document.createElement("section");
      panel.className = "panel message search-panel";
      panel.appendChild(div);
      results.replaceChildren(panel);
      setDrawer(true);
    } else {
      target.replaceChildren(div);
    }
  }
  ["htmx:sendError", "htmx:timeout"].forEach(function (ev) {
    document.addEventListener(ev, function (e) {
      showFailure(e.detail.target, "Couldn’t reach the server. Check your connection and try again.");
    });
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
