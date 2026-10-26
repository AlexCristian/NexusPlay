// Nexus Play watch page: loads the video (directly or through HLS), resumes
// playback, saves progress, and opens the subtitle correction dialog.
(function () {
  "use strict";

  var video = document.getElementById("player");
  if (!video) return;

  var mediaID = video.dataset.mediaId;
  var src = video.dataset.src;
  var useHLS = video.dataset.hls === "true";
  var resumeAt = parseFloat(video.dataset.resume) || 0;
  var signedIn = video.dataset.signedIn === "true";
  var status = document.getElementById("player-status");
  var spinner = document.getElementById("player-spinner");
  var topBar = document.getElementById("top-bar");
  var reportButton = document.getElementById("report-subtitle");

  // showStatus puts an error over the video; "" clears it. Loading is shown
  // by the spinner, never by text.
  function showStatus(text) {
    status.textContent = text;
    status.hidden = !text;
    if (text) spinner.hidden = true;
  }

  // The session's CSRF token. The server puts it in <body hx-headers> so htmx
  // sends it on every request (see csrfHeaders in views.go); player.js posts
  // with sendBeacon, which cannot set headers, so it reads the token from
  // there and sends it as the csrf_token form field instead.
  function csrfToken() {
    try {
      return JSON.parse(document.body.getAttribute("hx-headers") || "{}")["X-CSRF-Token"] || "";
    } catch (e) {
      return "";
    }
  }

  // Loading -----------------------------------------------------------------

  function loadScript(url) {
    return new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.src = url;
      s.onload = resolve;
      s.onerror = reject;
      document.head.appendChild(s);
    });
  }

  // hls.js handles resuming itself (startPosition); everything else resumes
  // on loadedmetadata below.
  var usingHlsJs = false;

  function nativeHLS() {
    if (!video.canPlayType("application/vnd.apple.mpegurl")) {
      showStatus("This browser cannot play this video.");
      return;
    }
    video.src = src;
  }

  function startHLS() {
    // Prefer hls.js wherever Media Source Extensions exist. Recent Chrome also
    // claims native HLS support, but its native player fails on a playlist
    // that ffmpeg is still writing; native HLS is the fallback for iOS.
    if (!window.MediaSource && !window.ManagedMediaSource) {
      nativeHLS();
      return;
    }
    loadScript(video.dataset.hlsScript).then(function () {
      if (!window.Hls || !window.Hls.isSupported()) {
        nativeHLS();
        return;
      }
      usingHlsJs = true;
      var hls = new window.Hls({
        manifestLoadingTimeOut: 70000, // the first request waits for ffmpeg to start
        manifestLoadingMaxRetry: 2,
        // While ffmpeg is still writing, the playlist is an open "event"
        // playlist that hls.js would treat as live and join near its end.
        // Always start at the beginning (or the resume point) instead.
        startPosition: resumeAt > 5 ? resumeAt : 0
      });
      hls.on(window.Hls.Events.ERROR, function (_, data) {
        if (!data.fatal) return;
        if (data.response && data.response.code === 503) {
          showStatus("The server is busy converting other videos. Try again in a minute.");
        } else if (data.type === window.Hls.ErrorTypes.MEDIA_ERROR) {
          hls.recoverMediaError();
        } else {
          showStatus("This video could not be loaded.");
        }
      });
      hls.loadSource(src);
      hls.attachMedia(video);
    }).catch(function () {
      showStatus("The video player failed to load.");
    });
  }

  if (useHLS) {
    startHLS();
  } else {
    video.src = src;
  }

  video.addEventListener("loadeddata", function () { showStatus(""); });

  // The spinner shows while the viewer is waiting for video: after asking
  // to play (or seeking) until frames arrive. Until then the play button
  // shows; iOS loads nothing before the first tap.
  function wait(on) { spinner.hidden = !on || !status.hidden; }
  video.addEventListener("play", function () { wait(video.readyState < 3); });
  video.addEventListener("waiting", function () { wait(!video.paused); });
  video.addEventListener("seeking", function () { wait(!video.paused); });
  ["playing", "canplay", "seeked", "pause", "ended", "error"].forEach(function (ev) {
    video.addEventListener(ev, function () { wait(false); });
  });
  video.addEventListener("error", function () {
    if (video.error) showStatus("This video could not be played.");
  });

  // Resume where the viewer left off.
  var resumed = false;
  video.addEventListener("loadedmetadata", function () {
    if (resumed || usingHlsJs) return;
    resumed = true;
    if (resumeAt > 5 && (!video.duration || resumeAt < video.duration - 10)) {
      video.currentTime = resumeAt;
    }
  });

  // Dim the top bar while playing, as the original player did.
  // The browser's own bar (the iPhone status bar area) follows, through
  // theme-color, so it matches the top bar instead of fading separately.
  var themeColor = document.querySelector('meta[name="theme-color"]');
  var idleColor = themeColor ? themeColor.content : "";
  function setPlayingBar(playing) {
    topBar.classList.toggle("playing", playing);
    if (themeColor) themeColor.content = playing ? "#191919" : idleColor;
    retintBrowserBar();
  }

  // Safari 26 ignores theme-color and paints its own bar in the color of
  // the sticky top bar, but it only looks again when a fixed or sticky
  // element appears or disappears (opening a popover, say). So after the
  // top bar changes color, a fixed element shown for a moment makes it
  // look: otherwise a popover opened while playing left Safari's bar dark
  // after pausing.
  function retintBrowserBar() {
    var probe = document.createElement("div");
    probe.className = "retint-probe";
    probe.setAttribute("aria-hidden", "true");
    document.body.appendChild(probe);
    requestAnimationFrame(function () {
      requestAnimationFrame(function () { probe.remove(); });
    });
  }
  video.addEventListener("play", function () { setPlayingBar(true); });
  video.addEventListener("pause", function () { setPlayingBar(false); });
  video.addEventListener("ended", function () { setPlayingBar(false); });

  // Controls ----------------------------------------------------------------
  //
  // The control bar in the original's style replaces the browser's own
  // controls. Without this script the browser's controls stay.

  var container = document.getElementById("player-container");
  var controls = document.getElementById("controls");
  var bigPlay = document.getElementById("big-play");
  var seek = document.getElementById("ctl-seek");
  var volume = document.getElementById("ctl-volume");
  var timeNow = document.getElementById("ctl-time");
  var timeTotal = document.getElementById("ctl-duration");
  var captionsButton = document.getElementById("ctl-captions");
  var captionsMenu = document.getElementById("captions-menu");
  var seeking = false;
  // The length from the server. While a file is converted on the fly, the
  // browser only knows the part converted so far, so the server's length is
  // what the time and the seek bar show.
  var knownDuration = parseFloat(video.dataset.duration) || 0;

  function totalDuration() {
    var d = video.duration;
    if (isFinite(d) && d > 0 && d >= knownDuration - 1) return d;
    return knownDuration || (isFinite(d) ? d : 0);
  }

  function clock(sec) {
    if (!isFinite(sec) || sec < 0) sec = 0;
    sec = Math.floor(sec);
    var h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60, s = sec % 60;
    var mm = (m < 10 ? "0" : "") + m, ss = (s < 10 ? "0" : "") + s;
    return h > 0 ? h + ":" + mm + ":" + ss : mm + ":" + ss;
  }

  function togglePlay() {
    if (video.paused || video.ended) {
      var p = video.play();
      if (p && p.catch) p.catch(function () {});
    } else {
      video.pause();
    }
  }

  function showTime() {
    var d = totalDuration();
    timeNow.textContent = clock(video.currentTime);
    timeTotal.textContent = clock(d);
    if (!seeking && isFinite(d) && d > 0) seek.value = String(Math.round(video.currentTime / d * 1000));
    seek.style.setProperty("--fill", (seek.value / 10) + "%");
    if (isFinite(d) && d > 0 && video.buffered.length) {
      var end = video.buffered.end(video.buffered.length - 1);
      seek.style.setProperty("--loaded", Math.min(100, end / d * 100) + "%");
    }
  }

  function showPlaying() {
    var playing = !video.paused && !video.ended;
    container.classList.toggle("is-playing", playing);
    bigPlay.hidden = playing;
    document.getElementById("ctl-play").setAttribute("aria-label", playing ? "Pause" : "Play");
  }

  function showVolume() {
    var muted = video.muted || video.volume === 0;
    container.classList.toggle("is-muted", muted);
    volume.value = String(muted ? 0 : video.volume);
    volume.style.setProperty("--fill", (muted ? 0 : video.volume * 100) + "%");
  }

  video.controls = false;
  controls.hidden = false;
  showPlaying();
  showVolume();

  video.addEventListener("click", togglePlay);
  bigPlay.addEventListener("click", togglePlay);
  document.getElementById("ctl-play").addEventListener("click", togglePlay);
  ["play", "playing", "pause", "ended", "emptied"].forEach(function (ev) { video.addEventListener(ev, showPlaying); });
  ["timeupdate", "durationchange", "progress", "loadedmetadata"].forEach(function (ev) { video.addEventListener(ev, showTime); });
  video.addEventListener("volumechange", showVolume);

  seek.addEventListener("input", function () {
    seeking = true;
    seek.style.setProperty("--fill", (seek.value / 10) + "%");
    timeNow.textContent = clock(seek.value / 1000 * totalDuration());
  });
  seek.addEventListener("change", function () {
    var target = seek.value / 1000 * totalDuration();
    // Not converted that far yet: go as far as there is.
    if (isFinite(video.duration) && target > video.duration - 1) target = Math.max(0, video.duration - 1);
    video.currentTime = target;
    seeking = false;
    showTime();
  });

  volume.addEventListener("input", function () {
    video.volume = parseFloat(volume.value);
    video.muted = video.volume === 0;
  });
  document.getElementById("ctl-mute").addEventListener("click", function () {
    if (video.volume === 0) video.volume = 1;
    video.muted = !video.muted;
  });

  document.getElementById("ctl-fullscreen").addEventListener("click", function () {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else if (container.requestFullscreen) {
      container.requestFullscreen().catch(function () {});
    } else if (video.webkitEnterFullscreen) {
      video.webkitEnterFullscreen(); // iPhone: only the video itself can go full screen
    }
  });
  video.addEventListener("dblclick", function () { document.getElementById("ctl-fullscreen").click(); });

  // Space and the arrow keys work anywhere on the page except in a field.
  document.addEventListener("keydown", function (e) {
    if (e.target.closest("input, textarea, select, button, a, dialog[open]") || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === " " || e.key === "k") { e.preventDefault(); togglePlay(); }
    else if (e.key === "ArrowRight") { video.currentTime = Math.min(video.duration || 0, video.currentTime + 10); }
    else if (e.key === "ArrowLeft") { video.currentTime = Math.max(0, video.currentTime - 10); }
    else if (e.key === "f") { document.getElementById("ctl-fullscreen").click(); }
  });

  // Subtitles ---------------------------------------------------------------
  //
  // The player draws subtitles itself (#cues), in a padded, rounded box
  // above the control bar as in the original: the browser's own rendering
  // cannot be padded, rounded or lifted. The chosen track is kept in
  // "hidden" mode, which loads its cues without the browser drawing them.
  // iPhone's native fullscreen shows only the browser's subtitles, so the
  // track switches to "showing" there and back afterwards.
  var cuesBox = document.getElementById("cues");
  var activeTrack = null;
  var nativeFullscreen = false;

  function subtitleTracks() {
    return Array.prototype.filter.call(video.textTracks, function (t) { return t.id.indexOf("sub-") === 0; });
  }

  function renderCues() {
    cuesBox.replaceChildren();
    if (!activeTrack || nativeFullscreen || !activeTrack.activeCues) return;
    Array.prototype.forEach.call(activeTrack.activeCues, function (cue) {
      var line = document.createElement("div");
      var box = document.createElement("span");
      box.appendChild(cue.getCueAsHTML()); // a safe fragment: text and <i>/<b> only
      line.appendChild(box);
      cuesBox.appendChild(line);
    });
  }

  function setSubtitles(track) {
    activeTrack = track;
    subtitleTracks().forEach(function (t) {
      t.mode = t !== track ? "disabled" : (nativeFullscreen ? "showing" : "hidden");
      t.oncuechange = t === track ? renderCues : null;
    });
    container.dataset.subtitles = track ? track.id : "";
    if (captionsButton) captionsButton.classList.toggle("on", !!track);
    renderCues();
    if (typeof updateReport === "function") updateReport();
  }

  // A track turned on some other way (the browser's own menu) is adopted.
  video.textTracks.addEventListener("change", function () {
    if (nativeFullscreen) return;
    var shown = subtitleTracks().filter(function (t) { return t.mode === "showing"; })[0];
    if (shown) setSubtitles(shown);
  });
  video.addEventListener("webkitbeginfullscreen", function () { nativeFullscreen = true; setSubtitles(activeTrack); });
  video.addEventListener("webkitendfullscreen", function () { nativeFullscreen = false; setSubtitles(activeTrack); });

  function buildCaptionsMenu() {
    var tracks = subtitleTracks();
    var showing = activeTrack;
    var items = [{ label: "Off", track: null }].concat(tracks.map(function (t) { return { label: t.label || t.language, track: t }; }));
    captionsMenu.replaceChildren.apply(captionsMenu, items.map(function (item) {
      var b = document.createElement("button");
      b.type = "button";
      b.setAttribute("role", "menuitemradio");
      b.setAttribute("aria-checked", String(item.track === showing));
      b.textContent = item.label;
      b.addEventListener("click", function () {
        setSubtitles(item.track);
        closeCaptionsMenu();
      });
      return b;
    }));
  }

  function closeCaptionsMenu(refocus) {
    captionsMenu.hidden = true;
    captionsButton.setAttribute("aria-expanded", "false");
    if (refocus) captionsButton.focus();
  }

  if (captionsButton) {
    captionsButton.addEventListener("click", function (e) {
      e.stopPropagation();
      if (!captionsMenu.hidden) { closeCaptionsMenu(); return; }
      buildCaptionsMenu();
      captionsMenu.hidden = false;
      captionsButton.setAttribute("aria-expanded", "true");
      // From the keyboard, focus moves into the menu, on the current choice.
      if (e.detail === 0) (captionsMenu.querySelector('[aria-checked="true"]') || captionsMenu.querySelector("button")).focus();
    });
    // Escape closes the menu and returns focus to its button.
    captionsMenu.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { e.stopPropagation(); closeCaptionsMenu(true); }
    });
    document.addEventListener("click", function (e) {
      if (!captionsMenu.hidden && !captionsMenu.contains(e.target)) closeCaptionsMenu();
    });
  }

  // While playing, the controls and the pointer fade out after 2.5 s
  // without movement, as in the original; any movement, tap or key brings
  // them back. Paused, or with focus in the controls, they stay.
  var idleTimer = 0;
  function wake() {
    container.classList.remove("idle");
    clearTimeout(idleTimer);
    idleTimer = setTimeout(function () {
      var busy = video.paused || video.ended || controls.contains(document.activeElement) ||
        (captionsMenu && !captionsMenu.hidden);
      if (!busy) container.classList.add("idle");
    }, 2500);
  }
  ["mousemove", "pointerdown", "touchstart", "keydown", "focusin"].forEach(function (ev) {
    container.addEventListener(ev, wake, { passive: true });
  });
  video.addEventListener("play", wake);
  ["pause", "ended"].forEach(function (ev) {
    video.addEventListener(ev, function () { clearTimeout(idleTimer); container.classList.remove("idle"); });
  });

  // Progress ----------------------------------------------------------------
  //
  // The position is saved every 15 s while playing, on pause, when the page
  // is hidden or closed, and at the end.
  // "ended" sends finished=1. The server also counts 95% as finished,
  // because a viewer who closes the tab during the credits never reaches
  // "ended" (see saveProgress in handlers_playback.go).

  if (signedIn) {
    var lastSaved = -1;
    var save = function (finished) {
      if (!finished && (video.currentTime < 1 || Math.abs(video.currentTime - lastSaved) < 1)) return;
      lastSaved = video.currentTime;
      var data = new FormData();
      data.append("media_id", mediaID);
      data.append("position", video.currentTime.toFixed(1));
      data.append("csrf_token", csrfToken());
      if (finished) data.append("finished", "1");
      // sendBeacon survives the page being closed, unlike a normal request.
      if (!navigator.sendBeacon || !navigator.sendBeacon("/me/progress", data)) {
        fetch("/me/progress", { method: "POST", body: data, keepalive: true, credentials: "same-origin" });
      }
    };
    setInterval(function () { if (!video.paused) save(false); }, 15000);
    video.addEventListener("pause", function () { if (!video.ended) save(false); });
    video.addEventListener("ended", function () { save(true); });
    window.addEventListener("pagehide", function () { if (!video.ended) save(false); });
    document.addEventListener("visibilitychange", function () {
      if (document.visibilityState === "hidden" && !video.ended) save(false);
    });
  }

  // Subtitle corrections ----------------------------------------------------

  function showingTrack() { return activeTrack; }

  function updateReport() {
    if (reportButton) reportButton.hidden = !activeTrack;
  }
  updateReport();

  if (reportButton) {
    reportButton.addEventListener("click", function () {
      var track = showingTrack();
      if (!track) return;
      video.pause();
      var ms = Math.floor(video.currentTime * 1000);
      var url = "/media/" + mediaID + "/subtitle-report?track=" + track.id.slice(4) + "&t=" + ms;
      window.htmx.ajax("GET", url, { target: "#modal-body" });
    });
  }
})();
