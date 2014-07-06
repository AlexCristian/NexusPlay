/**
 * Prevent video seek reset loop
 */
var hasResetFrame = false;
$(document).ready(function(){
		
		/**
		 * Initialize player
		 */
		$('audio,video').mediaelementplayer({
			videoWidth: '100%',
			videoHeight: '100%',
			audioWidth: '100%',
			features: ['playpause','progress','tracks','volume'],
			videoVolume: 'horizontal',
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

		    	/**
		    	 * AJAX call to the server in order to store the current
		    	 * playback status.
		    	 */
		    	function storePlaybackState(){
		    		if(mediaElement.currentTime != mediaElement.duration){
						$.ajax({
							type: "GET",
							url: "./StorePlaybackState",
							data: { 
								id : mediaID,
								time : mediaElement.currentTime
							}
						});
		    		}
				}
		    	
		    	/**
		    	 * Toggle navbar theme
		    	 */
		    	function onPlay(){
			    	$('#topBar').toggleClass("normalTopBar playTopBar");
			        $('.topBarButton').toggleClass("normalTopBarButton playTopBarButton");
			    }
			    
			    function onPause(){
			    	$('#topBar').toggleClass("normalTopBar playTopBar");
			        $('.topBarButton').toggleClass("normalTopBarButton playTopBarButton");
			        storePlaybackState();
			    }
			    
		       mediaElement.addEventListener('pause', onPause, false);

		       mediaElement.addEventListener('play', onPlay, false);
		       
		       /**
		        * Resume video from the time index stored on the server
		        */
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
				
		       /**
		        * Call AJAX function to store the time index at which
		        * the video stopped
		        */
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

				
				/**
				 * Enable responsive captions
				 */
				$(".mejs-captions-text").addClass("zeta");
				
				/**
				 * Some browsers don't support native WebVTT, while others do.
				 * Since fullscreen subtitling on iOS is only possible through native WebVTT,
				 * we'll conveniently switch to native subtitling when needed; otherwise
				 * we stick with the MediaElementJS implementation.
				 */
				try{
					/**
					 * Set default subtitle to none
					 */
					for (var i = 0; i < mediaElement.textTracks.length; i++) {
						mediaElement.textTracks[i].mode = 'hidden';
					}
					
					/**
					 * Know at all times what language (if any) is selected
					 */
					currentLang='none';
					$(".mejs-captions-selector > ul > li > input").click(function(){
						currentLang = this.value;						
					});
					
					/**
					 * Detect iOS fullscreen mode & switch on native captioning 
					 */
					mediaElement.addEventListener('webkitbeginfullscreen', function(){
						for (var i = 0; i < mediaElement.textTracks.length; i++) {
							mediaElement.textTracks[i].mode = 'hidden';
							if(mediaElement.textTracks[i].language == lang){
								mediaElement.textTracks[i].mode = 'showing';
							}
						}
					}, false);
					
					mediaElement.addEventListener('webkitendfullscreen', function(){
						for (var i = 0; i < mediaElement.textTracks.length; i++) {
							mediaElement.textTracks[i].mode = 'hidden';
						}
					}, false);
				}catch(err){
					console.log("Your browser doesn't support WebVTT");
				}
				
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
