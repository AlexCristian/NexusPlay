<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8" import="com.nexusplay.containers.Media,com.nexusplay.containers.User,com.nexusplay.db.MediaDatabase"%>
<script type="text/javascript">
	$(document).ready(function(){
		$('.menu-button').click(function (e) {
			  $(".controlContent").hide();
			  $("#" + $(this).data("target")).css("display","inline-block");
			});
	});
	function resizeIframe(obj) {
	    obj.style.height = obj.contentWindow.document.body.scrollHeight + 'px';
	    obj.style.width = obj.contentWindow.document.body.scrollWidth + 'px';
	  }
</script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.fineuploader-3.5.0.min.js'></script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/iframe.xss.response-3.5.0.js'></script>
<style>
.typeahead{
	margin-left:10px;
}
</style>
<% 
User user = (User) request.getAttribute("user");
%>
<div class="controlSelector">
	<div style="height:30px;"></div>
	<div class="menu-button" data-target="profile">Profile</div>
	<div class="menu-button" data-target="mediamng">Media management</div>
	<div class="menu-button" data-target="collections">Collections</div>
</div>
<div class="controlContent" id="profile" style="display:inline-block;">
	<h2 class='featurette-heading'><%= user.getNickname() %><span class="muted">'s profile</span></h2>
	<hr class="featurette-divider">
	<br><br>
</div>
<div class="controlContent" id="mediamng">
	<h2>Media management panel</h2>
	<hr class="featurette-divider">
	<h5>Uncategorized media</h5>
	<% 
		Media[] unpublished = MediaDatabase.getUnpublishedMedia();
		for(Media item : unpublished){
		%>	
			<iframe src="EditMedia?media=<%=item.getId() %>" seamless frameborder="0" scrolling="no" onload='javascript:resizeIframe(this);'></iframe>
		<%
		}
	%>
</div>
<div class="controlContent" id="collections">
	<h2>Manage collections</h2>
	<hr class="featurette-divider">
	<div class="well uncategHolder" style="text-align: center;">
	<p><b><i>Add a collection</i></b></p>
		<form id="collAdd" target="transFrame" method="POST" action="./AddCollection" enctype="multipart/form-data" >
		<table class="adminTable">
			<tr>
				<td>Name</td>
				<td><input id="collnm" type="text" name="Name" placeholder="Name" style="margin-bottom:10px; margin-left:10px;"></td>
			</tr>
			<tr>
				<td>Year</td>
				<td><input id="collyr" type="text" name="Year" placeholder="Year" style="margin-bottom:10px; margin-left:10px;"></td>
			</tr>
			<tr>
				<td>Poster</td>
				<td>
					<div style="margin-left:50px;"><input type="file" name="datafile" size="40"></div>
				</td>
			</tr>
			<tr>
				<td></td>
				<td><div id="addCollection" class="submitButton pull-right topBarButton normalTopBarButton">Add</div></td>
			</tr>
		</table>
		</form>
		<iframe style="display:none;" name="transFrame" id="transFrame"></iframe>
		<script type="text/javascript">
						$(document).ready(function(){
							$("#addCollection").click(function(){
								$("#collAdd").attr("action", "./AddCollection?name=" + $("#collnm").val() + "&year=" + $("#collyr").val());
								$("#collAdd").submit();
							});
						});
					</script>
	</div>
</div>