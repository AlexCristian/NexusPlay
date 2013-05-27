function displayCollection(id){
	$.ajax({
		type: "POST",
		url: "./CollectionDisplayer",
		data: { id : id}
	}).done(function( data ){
		$("#collection").html(data);
		$("#collection").modal();
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