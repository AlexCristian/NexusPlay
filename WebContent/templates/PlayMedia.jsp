<%@page import="com.nexusplay.db.CollectionsDatabase"%>
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
<div class="videoContent">
	<script>
	$(document).ready(function(){
		jQuery("#mediaName").fitText(2);
	});
	</script>
	<div class="metadata">
		<img src=".<%=item.getPoster() %>" class="watchPoster" width="120px"/>
		<% if(item.getCollectionID().isEmpty()){ %>
		<div class="metaText">
			<h2 id="mediaName"><%=item.getName() %></h2>
			<h5 class="zeta">Released in <%=item.getYear() %></h5>
		</div>
		<% }else{ %>
		<div class="metaText">
			<h2 id="mediaName"><%=CollectionsDatabase.matchIdWithName(item.getCollectionID()) %><span class="muted"> - <%=item.getName() %></span></h2>
			<h5 class="zeta">Season <%=item.getSeason() %>, episode <%=item.getEpisode() %></h5>
		</div>
		<% } %>
	</div>
	<h5 style="margin:0;">Comments</h5>
</div>
<% item=null; %>