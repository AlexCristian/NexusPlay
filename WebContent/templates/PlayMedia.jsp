<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8" import="com.nexusplay.containers.*, com.nexusplay.db.MediaDatabase"%>
<div class='playerContainer'>
	<%  
		Media item = (Media) request.getAttribute("media");
		User user = (User) request.getAttribute("user");
	%>
	<script>
		var mediaID = "<%=request.getParameter("w") %>";
		var resumeLocation = "<%= ((user!=null) ? user.getPausedTimeByID(request.getParameter("w")) : "0") %>";
	</script>

	<video id="media-player" src="<%=request.getContextPath()+SettingsContainer.getMediaSource() %>/<%=item.getFilename() %>" height="400px" width="100%"></video>
</div>
<% item=null; %>
<br>
<h2>Now playing</h2>