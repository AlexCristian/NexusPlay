var hasResetFrame = false;	
$(document).ready(function(){
		
		$('audio,video').mediaelementplayer({
			videoWidth: '100%',
			videoHeight: '100%',
			audioWidth: '100%',
			features: ['playpause','progress','tracks','volume'],
			videoVolume: 'horizontal',
			mode: "shim",
		    loop: false,
		    // enables Flash and Silverlight to resize to content size
		    enableAutosize: true,
		    // the order of controls you want on the control bar (and other plugins below)
		    features: ['playpause','progress','current','duration','tracks','volume','fullscreen'],
		    // Hide controls when playing and mouse is not over the video
		    alwaysShowControls: false,
		    // force iPad's native controls
		    iPadUseNativeControls: false,
		    // force iPhone's native controls
		    iPhoneUseNativeControls: false, 
		    // force Android's native controls
		    AndroidUseNativeControls: false,
		    // forces the hour marker (##:00:00)
		    alwaysShowHours: false,
		    // show framecount in timecode (##:00:00:00)
		    showTimecodeFrameCount: false,
		    // used when showTimecodeFrameCount is set to true
		    framesPerSecond: 25,
		    // turns keyboard support on and off for this instance
		    enableKeyboard: true,
		    // when this player starts, it will pause other players
		    pauseOtherPlayers: true,
		    // array of keyboard commands
		    keyActions: [],
		 	// method that fires when the Flash or Silverlight object is ready
		    success: function (mediaElement, domObject) {   

		    	function storePlaybackState(){
					$.ajax({
						type: "GET",
						url: "./StorePlaybackState",
						data: { 
							id : mediaID,
							time : mediaElement.currentTime
						}
					});
				}
		    	function onPlay(){
			    	$('#topBar').toggleClass("normalTopBar playTopBar");
			        $('.topBarButton').toggleClass("normalTopBarButton playTopBarButton");
			    }
			    
			    function onPause(){
			    	$('#topBar').toggleClass("normalTopBar playTopBar");
			        $('.topBarButton').toggleClass("normalTopBarButton playTopBarButton");
			        storePlaybackState();
			    }
			    
		       // add event listeners
		       mediaElement.addEventListener('pause', function(e) {
		    	   onPause();
		       }, false);

		       mediaElement.addEventListener('play', function(e) {
		    	   onPlay();
		       }, false);
		       
		       
		       mediaElement.addEventListener("canplay", function() {
					if(!hasResetFrame){
			    	   setTimeout(
								  function() 
								  {
									  mediaElement.setCurrentTime(resumeLocation);
								  }, 1000);
			    	   hasResetFrame=true;
					}
				});
				
		       mediaElement.addEventListener("ended", function() {
					$.ajax({
						type: "GET",
						url: "./StorePlaybackState",
						data: { 
							id : mediaID,
							time : "0"
						}
					});
				});
		       
		       document.addEventListener("beforeunload", function() {
					storePlaybackState();
				});
				
				$(window).on('beforeunload', function(){
					storePlaybackState();
				});
				$(window).unload(function() {
					storePlaybackState();
				});
				$(window).on('beforeunload', function(e) {
					storePlaybackState();               
			    });
				
		   }
		});
		
		/**
		 * Adapt the player's size to the available space
		 */
		$(".playerContainer").css("height", $(".playerContainer").width() * 9 / 16-20);
		window.onresize = function(event) {
			$(".playerContainer").css("height", $(".playerContainer").width() * 9 / 16-20);
		};
	    
	});
