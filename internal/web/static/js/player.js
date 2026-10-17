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
  var topBar = document.getElementById("top-bar");
  var reportButton = document.getElementById("report-subtitle");

  function showStatus(text) {
    status.textContent = text;
    status.hidden = !text;
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
    showStatus("Preparing video…");
    // Prefer hls.js wherever Media Source Extensions exist. Recent Chrome also
    // claims native HLS support, but its native player fails on a playlist
    // that ffmpeg is still writing; native HLS is the fallback for iOS.
    if (!window.MediaSource && !window.ManagedMediaSource) {
      nativeHLS();
      return;
    }
    loadScript("/static/vendor/hls.light.min.js").then(function () {
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
  video.addEventListener("play", function () { topBar.classList.add("playing"); });
  video.addEventListener("pause", function () { topBar.classList.remove("playing"); });
  video.addEventListener("ended", function () { topBar.classList.remove("playing"); });

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

  function showingTrack() {
    for (var i = 0; i < video.textTracks.length; i++) {
      var t = video.textTracks[i];
      if (t.mode === "showing" && t.id.indexOf("sub-") === 0) return t;
    }
    return null;
  }

  if (reportButton) {
    var updateReport = function () { reportButton.hidden = !showingTrack(); };
    video.textTracks.addEventListener("change", updateReport);
    updateReport();

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
