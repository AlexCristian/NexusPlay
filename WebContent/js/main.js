function displayCollection(id){
	$.ajax({
		type: "POST",
		url: "./CollectionDisplayer?id=" + id
	}).done(function( data ){
		$("#collection").html(data);
		$("#collection").modal();
		$('.seasonSelector').on('click', function(event){
			id=$(this).attr('id').substring(7); maxwidth=$(".seasonSelector").size()*$(this).width();
	    	$('.seasonSelector > h5').css('color', "rgb(160, 160, 160)");
	    	$('.seasonContent').fadeOut(200, function(){
	    		$('#collsescnt'+id).fadeIn(200);
	    		});
	    	$('#'+$(this).attr('id')+' > h5').css('color', "white");
	    	
			leftOffset = ($(".seasonsSlider").width()/2-$(this).width()*(id));
			if($(this).width()*(id)>$(".seasonsSlider").width()){
			if(leftOffset<0&&maxwidth+leftOffset>$(".seasonsSlider").width()-30)
				$(".seasonsHolder").css('left', leftOffset + "px");
			else if(leftOffset>=0)
				$(".seasonsHolder").css('left', "0px");
			else if(maxwidth+leftOffset<=$(".seasonsSlider").width()-30)
				$(".seasonsHolder").css('left',$(".seasonsSlider").width()-30 - maxwidth +"px");
			}
		});
		$('.poster-wrapper').on('click', function(event){
			$.ajax({
				type: "POST",
				url: "./AddRemoveSubscription?id=" + id
			}).done(function(data){
				$('.ribbon').toggleClass("ribbon-green ribbon-blue");
			});
		});
	});
	
	
}
$(document).ready(function(){
	$("#login-submit").click(function(){
		$.ajax({
			type: "POST",
			url: "./Login",
			data: { email : $("#login-email-field").val(),
				password : CryptoJS.SHA1($("#login-password-field").val()).toString()}
		}).done(function( data ){
			$("#login-alert-content").html(data);
			$("#login-alert").fadeIn();
			if(data.substr(0,41)=="Welcome back! Loading your preferences...")
				location.reload();
		});
	});
	$("#register-submit").click(function(){
		$.ajax({
			type: "POST",
			url: "./Register",
			data: { email : $("#register-email-field").val(),
				password : CryptoJS.SHA1($("#register-password-field").val()).toString(),
				nickname : $("#register-nickname-field").val()
			}
		}).done(function( data ){
			$("#register-alert-content").html(data);
			$("#register-alert").fadeIn();
		});
	});
	$('body').on('click', function (e) {
	    $('.popover-link').each(function () {
	        //the 'is' for buttons that triggers popups
	        //the 'has' for icons within a button that triggers a popup
	        if (!$(this).is(e.target) && $(this).has(e.target).length === 0 && $('.popover').has(e.target).length === 0) {
	            $(this).popover('hide');
	        }
	    });
	});
});