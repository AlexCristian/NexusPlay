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
	<div class="metadata">
		<img src=".<%=item.getPoster() %>" width=140px" class="watchPoster"/>
		<% if(item.getCollectionID().isEmpty()){ %>
		<div class="metaText">
			<h2><%=item.getName() %></h2>
			<h5>Released in <%=item.getYear() %></h5>
		</div>
		<% }else{ %>
		<div class="metaText">
			<h2 class="gamma"><%=CollectionsDatabase.matchIdWithName(item.getCollectionID()) %><span class="muted"> - <%=item.getName() %></span></h2>
			<h5 class="zeta">Season <%=item.getSeason() %>, episode <%=item.getEpisode() %></h5>
		</div>
		<% } %>
	</div>
	<h5 style="margin:0;">Comments</h5>
</div>
<% item=null; %>