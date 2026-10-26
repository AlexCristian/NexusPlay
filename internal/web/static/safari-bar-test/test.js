  var strategies = {
    A: "What's deployed now: flash a hidden fixed element after each change",
    B: "Page background follows the bar, and the bar has a backdrop filter",
    C: "Page background follows the bar, and the bar element is replaced on each change",
    D: "Page background follows the bar, nothing else",
  };
  var current = (location.search.match(/[?&]s=([A-D])/) || [])[1] || "A";
  var list = document.getElementById("strategies");
  Object.keys(strategies).forEach(function (k) {
    var li = document.createElement("li");
    var a = document.createElement("a");
    a.href = "?s=" + k;
    a.innerHTML = "<b>" + k + "</b>" + strategies[k];
    if (k === current) a.setAttribute("aria-current", "page");
    li.appendChild(a);
    list.appendChild(li);
  });
  if (current === "B") document.documentElement.classList.add("with-backdrop");

  var themeColor = document.querySelector('meta[name="theme-color"]');
  var playing = false;
  document.getElementById("toggle").addEventListener("click", function () {
    playing = !playing;
    var bar = document.getElementById("top-bar");
    if (current === "C") {
      var fresh = bar.cloneNode(true);
      bar.replaceWith(fresh);
      bar = fresh;
    }
    bar.classList.toggle("playing", playing);
    themeColor.content = playing ? "#191919" : "#c21f00";
    if (current !== "A") document.documentElement.style.backgroundColor = playing ? "#191919" : "#c21f00";
    if (current === "A") {
      var probe = document.createElement("div");
      probe.style.cssText = "position:fixed;top:0;left:0;width:1px;height:1px;visibility:hidden;pointer-events:none";
      document.body.appendChild(probe);
      requestAnimationFrame(function () { requestAnimationFrame(function () { probe.remove(); }); });
    }
    this.textContent = playing ? "Pause" : "Play";
    document.getElementById("state").textContent = "Version " + current + ": top bar " + (playing ? "dark" : "red");
  });
