<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8" import="com.nexusplay.containers.Media"%>
<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
<html>
<head>
		<title>Alex's Nexus</title>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.color.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/sha1.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/fittext.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/main.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/player/lib/mediaelement.js'></script>
		<script type='text/javascript' src='<%=request.getContextPath()%>/player/lib/mediaelementplayer.js'></script>		
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/bootstrap.js'></script>
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/player/skin/mediaelementplayer.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap.min.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap-responsive.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/typeplate-unminified.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/default.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/player.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/fineuploader-3.5.0.css' />
	</head>
<body style = "background: none !important;">
<script type="text/javascript">
	$(document).ready(function(){
		$('.menu-button').click(function (e) {
			  $(".controlContent").hide();
			  $("#" + $(this).data("target")).css("display","inline-block");
			});
		$('input:checkbox').change(
			    function(){
			        if ($(this).is(':checked')) {
			        	$("." + $(this).attr('id')+"h").show();
			        }else{
			        	$("." + $(this).attr('id')+"h").hide();
			        }
			    });
	});
</script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.fineuploader-3.5.0.min.js'></script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/iframe.xss.response-3.5.0.js'></script>

	<%
		Media item = (Media) request.getAttribute("media");
		int i = 1;
	%>
	<div class="well uncategHolder" id="<%= item.getId() %>hold">
		<h5><%= item.getFilename() %></h5>
		<p><b><i>Suggested metadata</i></b></p>
		<form name="PublishMedia" action="PublishMedia" method="post" enctype="multipart/form-data">
			<div class="adminPosterHolder">
				<img style="display: inline-table; width:135px;" src=".<%= item.getPoster() %>"/>
				<input type="submit" id="<%= item.getId() %>" class="publish submitButton pull-right topBarButton normalTopBarButton" style="color:white;" value="Publish">
			</div>
			<table class="adminTable">
				<tr>
					<td>Name</td>
					<td><input id="<%= item.getId() %>n" type="text" name="Name" placeholder="Name" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getName() %>"></td>
				</tr>
				<tr>
					<td>Poster</td>
					<td><input type="file" size="50" name="poster" style="margin-bottom:10px; margin-left:10px;"></td>
				<tr>
					<td>Category</td>
					<td><input id="<%= item.getId() %>c" type="text" name="Category" placeholder="Category" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getCategory() %>"></td>
				</tr>
				<tr>
					<td>Year</td>
					<td><input id="<%= item.getId() %>y" type="text" name="Year" placeholder="Year" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getYear() %>"></td>
				</tr>
				<tr>
					<td>Collectible</td>
					<td><input id="<%= i %>" type="checkbox" name="Collectible" style="margin-bottom:10px; margin-left:10px;" onclick="javascript:parent.resizeIframe(this);"></td>
				</tr>
				<tr style="display:none;" class="<%= i %>h">
					<td>Collection</td>
					<script>
					$(document).ready(function(){
						$("#<%= item.getId() %>cll").typeahead({source:function ( query, process ){
							$.get( './CollectionSearch', { q: query }, function ( data ) {
								process( data.split("\n") );
								
							});
						}});
					});
					</script>
					<td><input id="<%= item.getId() %>cll" class="collectionSelector" autocomplete="off" type="text" name="Collection" placeholder="Collection" style="margin-bottom:10px; margin-left:10px;"></td>
				</tr>
				<tr style="display:none;" class="<%= i %>h">
					<td>Season</td>
					<td><input id="<%= item.getId() %>sn"  type="text" name="Season" placeholder="Season" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getSeason() %>"></td>
				</tr>
				<tr style="display:none;" class="<%= i %>h">
					<td>Episode</td>
					<td><input id="<%= item.getId() %>ep" type="text" name="Episode" placeholder="Episode" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getEpisode() %>"></td>
				</tr>
			</table>
		</form>
	</div>

</body>
</html>