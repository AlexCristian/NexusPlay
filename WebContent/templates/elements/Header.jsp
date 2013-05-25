<%@page import="com.nexusplay.db.MediaDatabase"%>
<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8" import="com.nexusplay.containers.Media,com.nexusplay.containers.User,java.util.Map"%>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en">
	<head>
		<title>Alex's Nexus</title>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.color.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/sha1.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/main.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/player/lib/mediaelement.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/player/lib/mediaelementplayer.js'></script>		
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/player/skin/mediaelementplayer.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap.min.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap-responsive.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/typeplate-unminified.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/default.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/player.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/fineuploader-3.5.0.css' />
	</head>
	<body>
		<div id="login" class="modal hide fade">
		  <div class="modal-header">
		    <button type="button" class="close" data-dismiss="modal" aria-hidden="true">&times;</button>
		    <h5>Login</h3>
		  </div>
		  <div class="modal-body">
		  	<div style="width: 100%; text-align: center;">
		  		<div class="alert alert-info" id="login-alert" style="display:none;">  
				  <a class="close" data-dismiss="alert">×</a>  
				  <div id="login-alert-content"></div>  
				</div> 
		  		<form onsubmit="$('#login-submit').click(); return false;" >
				  <div class="input-prepend" style="text-align: center;">						
						<span class="add-on">@</span>
						<input id="login-email-field" type="email" name="email" placeholder="E-mail" style="margin-bottom:10px;"><br>
						<span class="add-on">?</span>
						<input id="login-password-field" type="password" name="password" placeholder="Password" style="margin-bottom:10px;"><br>
						<a class="pull-right topBarButton normalTopBarButton"><div type="submit" id="login-submit" style="font-size: 15px; color:white;">Log in</div></a>
						<input type="submit" style="visibility: hidden;"/>
		          </div>
	          	</form>
	        </div>
	          <p>Not a registered user? Please register here.</p>
		  </div>
		</div>
		
		<div id="register" class="modal hide fade">
		  <div class="modal-header">
		    <button type="button" class="close" data-dismiss="modal" aria-hidden="true">&times;</button>
		    <h5>Register</h3>
		  </div>
		  <div class="modal-body">
		  	<div style="width: 100%; text-align: center;">
		  		<div class="alert alert-info" id="register-alert" style="display:none;">  
				  <a class="close" data-dismiss="alert">×</a>  
				  <div id="register-alert-content"></div>  
				</div> 
		  		<form onsubmit="$('#register-submit').click(); return false;" >
				  <div class="input-prepend" style="text-align: center;">
						<span class="add-on">Aa</span>
						<input id="register-nickname-field" type="text" name="nickname" placeholder="Nickname" style="margin-bottom:10px;"><br>						
						<span class="add-on">@</span>
						<input id="register-email-field" type="email" name="email" placeholder="E-mail" style="margin-bottom:10px;"><br>
						<span class="add-on">?</span>
						<input id="register-password-field" type="password" name="password" placeholder="Password" style="margin-bottom:10px;"><br>
						<a class="pull-right topBarButton normalTopBarButton" id="register-submit"><div style="font-size: 15px; color:white;">Submit</div></a>
		          		<input type="submit" style="visibility: hidden;"/>
		          </div>
		         </form>
	        </div>
	          <p>Registering on Nexus Play lets you subscribe to your favorite media and resume interrupted playback from where you left off.</p>
		  </div>
		</div>

		<div id='topBar' class='normalTopBar navbar-static-top'>
			<div id='innerTopBar'>
				<a href='<%=request.getContextPath()%>'><h4 class='topBarButton normalTopBarButton zeta'>&#9658; Nexus Play</h4></a>
				<% 	
					User user = (User) request.getAttribute("user");
					if(user!=null){ 
				%>
				<a rel="popover" class="popover-link" id="previously-watching"><h4 class='zeta topBarButton normalTopBarButton'>Previously watching</h4></a>
				<%  
					String pausedList = new String();
					int m=0;
					
					for (Map.Entry<String, String> entry : user.getPaused().entrySet()) {
						m++;
						Media item = MediaDatabase.getMediaById(entry.getKey());
						if(m>1)
							pausedList+="<hr class=\"featurette-divider\">";
						pausedList+= "<a href=\"./watch?w=" + item.getId() + "\"><div class=\"pausedMedia\">";
						pausedList+= "<img src=\"." + item.getPoster() + "\" height=\"60px\"/>";
						pausedList+= "<div class=\"innerPausedMedia\">" + item.getName() + "<br><i class=\"icon-pause\"></i>" + (int) Float.parseFloat(entry.getValue())/60 + " min</div>";
						pausedList+= "</div></a>";
					}
				
				
				
				%>
				<script type='text/javascript'>
					$(document).ready(function(){
						$("#previously-watching").popover({
						      placement : 'bottom', //placement of the popover. also can use top, bottom, left or right
						      title : '<div style="text-align:center; color:black; font-size:14px;">Resume paused media</div>', //this is the top title bar of the popover. add some basic css
						      html: 'true', //needed to show html of course
						      content : '<div id="popOverBox"><%= pausedList %></div>' //this is the content of the html box. add the image here or anything you want really.
						});
					});
				</script>
				<a href='<%=request.getContextPath()%>/Subscriptions'><h4 class='zeta topBarButton normalTopBarButton'>My subscriptions</h4></a>
				<a href='<%=request.getContextPath()%>/ControlPanel' class="pull-right"><h4 class='zeta userBar topBarButton normalTopBarButton'>Welcome, <%= user.getNickname() %></h4></a>
				<% }else{ %>
				<a href='#register' class="pull-right" data-toggle="modal"><h4 class='zeta userBar topBarButton normalTopBarButton'>Register</h4></a>
				<a href='#login' class="pull-right" data-toggle="modal"><h4 class='zeta userBar topBarButton normalTopBarButton'>Log in</h4></a>
				<% } %>
			</div>
		</div>
		<div class='row-fluid'>
			<div class='span2'></div>
			<div class='span8 container' id='mainHolder'>
				<div id='abovePage'>
					<form class='pull-right' id="search">
						<input id="searchBox" type="text" size="40" placeholder="Search media..." />
					</form>
				</div>
				<script type='text/javascript'>
				$(document).ready(function(){
					$('#searchBox').bind('input', function() {
						$.ajax({url: '<%=request.getContextPath()%>/search?q=' + $('#searchBox').val(),
								context: document.body
								}).done(function( html ) {
									$('#searchResults').html(html); 
									if($('#searchBox').val()=='' || html=='') { 
										$('#searchResults > .categoriesHolder').prepend("<div class='categorySelector beta'>Search for </div>"); 
										$('#searchResults').slideUp(); $('#pageCover').animate({opacity:1}, 250);
									} else {
										$('#searchResults > .categoriesHolder').prepend("<div class='categorySelector beta'>Search for </div>"); 
										$('#searchResults').slideDown(); 
										$('#pageCover').animate({opacity:0.4}, 250);
									}
									});
					});
				});
				</script>
				<div class='contentContainer'>
					<div id='searchResults'></div>
					<div id='pageCover'>