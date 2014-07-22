<%@page import="com.nexusplay.db.CollectionsDatabase"%>
<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8" import="com.nexusplay.containers.*, com.nexusplay.db.MediaDatabase"%>
<div class='playerContainer'>
	<%  
		Media item = (Media) request.getAttribute("media");
		User user = (User) request.getAttribute("user");
		Subtitle[] subs = (Subtitle[]) request.getAttribute("subs");
	%>
	<script>
		var mediaID = "<%=request.getParameter("w") %>";
		var resumeLocation = "<%= ((user!=null) ? user.getPausedTimeByID(request.getParameter("w")) : "0") %>";
	</script>

	<video id="media-player" src="<%=request.getContextPath()+SettingsContainer.getMediaSource() %>/<%=item.getId() %>.<%=item.getFileFormat() %>" height="400px" width="100%">
		<%
			for(Subtitle sub : subs){
		%>
		 <track type="text/vtt" kind="subtitles" label="<%= sub.getLanguage() %>" src="<%=request.getContextPath()+SettingsContainer.getSubtitleSource() %>/<%= sub.getId() %>.<%= sub.getFileFormat() %>" srclang="<%= sub.getSourceLanguage()%>" default></track>
		<% } %>
	</video>
</div>
<div class="videoContent">
	<div class="playerActionTabsContainer">
		<div class="playerActionTab proposeChangeButton">
			<button type="button" aria-controls="mep_0" title="Report a subtitle error" aria-label="Report a subtitle error"></button>
		</div>
		<div class="playerActionTab editButton">
			<button type="button" aria-controls="mep_0" title="Edit metadata" aria-label="Edit metadata"></button>
		</div>
	</div>
	<div class="metadata">
		<table>
			<% if(item.getCollectionID().isEmpty()){ %>
			<tr>
				<td>
					<img src=".<%=item.getPoster() %>" class="watchPoster" width="120px"/>
				</td>
				<td>
					<div class="metaText">
						<h2 class="gamma"><%=item.getName() %></h2>
						<h5 class="zeta">Released in <%=item.getYear() %></h5>
					</div>
				</td>
			</tr>
			<% }else{ %>
			<tr>
				<td>
					<img src=".<%=item.getPoster() %>" class="watchPoster" width="120px" style="cursor:pointer" onClick="displayCollection('<%=item.getCollectionID() %>')"/>
				</td>
				<td>
					<div class="metaText">
						<h2 class="gamma"><%=CollectionsDatabase.matchIdWithName(item.getCollectionID()) %><span class="muted"> - <%=item.getName() %></span></h2>
						<h5 class="zeta">Season <%=item.getSeason() %>, episode <%=item.getEpisode() %></h5>
					</div>
				</td>
			</tr>
			<% } %>
		</table>
		
	</div>
	<!-- 
		This is where comments & subs will go
		<h5 style="margin:0;">Comments</h5>
	 -->
</div>
<% item=null; %>