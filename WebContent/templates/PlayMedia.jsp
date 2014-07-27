<%@page import="com.nexusplay.db.CollectionsDatabase"%>
<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8" import="com.nexusplay.containers.*, com.nexusplay.db.MediaDatabase"%>
<%  
		Media item = (Media) request.getAttribute("media");
		User user = (User) request.getAttribute("user");
		Subtitle[] subs = (Subtitle[]) request.getAttribute("subs");
		Change[][] changes = (Change[][]) request.getAttribute("changes");
		String[] correl = (String[]) request.getAttribute("changes-correlation");
%>
<script type="text/javascript">
function resizeIframe(id) {
		setTimeout(function(){
			obj = document.getElementById(id + 'ifrm');
		    obj.style.height = (obj.contentWindow.document.getElementById(id + "hold").scrollHeight + 20) + 'px';
		    obj.style.width = (obj.contentWindow.document.getElementById(id + "hold").scrollWidth + 20) + 'px';
		}, 1000);
	  };
var openSubtitleChangeModal, subIDs=[], voteChange;
<% for(Subtitle sub : subs){ %>
	subIDs["<%=sub.getSourceLanguage() %>"] = "<%= sub.getId() %>";
<% } %>
$(document).ready(function(){
	openSubtitleChangeModal = function(){
		document.getElementById("media-player").pause();
		$("#user-sub-original").html($(".mejs-captions-text").text());
		$("#subtitle-change-field").val($(".mejs-captions-text").text());
	};
	$(".proposeChangeButton").click(function(){
		$(".submitted-changes-container").hide();
		$("#changes-" + currentLang).show();
	});
	$(".editButton").click(function(){
		resizeIframe("<%=item.getId() %>");
	});
	$("#submit-suggestion").click(function(){
		$.ajax({
			type: "POST",
			url: "./SubmitChange",
			data: { originalContent : $("#user-sub-original").text(),
				changedContent : $("#subtitle-change-field").val(),
				subID : subIDs[currentLang] }
		}).done(function( data ){
			$("#error-modal-alert-content").html(data);
			$("#error-modal-alert").fadeIn();
		});
	});
	voteChange = function(id){
		$.ajax({
			type: "POST",
			url: "./VoteChange",
			data: { changeId : id,
				subId : subIDs[currentLang]}
		}).done(function( data ){
			$("#error-modal-alert-content").html(data);
			$("#error-modal-alert").fadeIn();
		});
	};
});
</script>
<div class='playerContainer'>
	<script>
		var mediaID = "<%=request.getParameter("w") %>";
		var resumeLocation = "<%= ((user!=null) ? user.getPausedTimeByID(request.getParameter("w")) : "0") %>";
	</script>

	<video id="media-player" src="<%=request.getContextPath()+SettingsContainer.getMediaSource() %>/<%=item.getId() %>.<%=item.getFileFormat() %>" height="400px" width="100%">
		<%
			for(Subtitle sub : subs){
		%>
		 <track type="text/vtt" kind="subtitles" id="subtitle-<%=sub.getSourceLanguage() %>" label="<%= sub.getLanguage() %>" src="<%=request.getContextPath()+SettingsContainer.getSubtitleSource() %>/<%= sub.getId() %>.<%= sub.getFileFormat() %>" srclang="<%= sub.getSourceLanguage()%>" default></track>
		<% } %>
	</video>
</div>
<div class="videoContent">
	<% if(user != null){ %>
	<div class="playerActionTabsContainer">
		<a class="playerActionTab proposeChangeButton" id="proposeChange" href="#subtitle-error-modal" data-toggle="modal" onclick="javascript:openSubtitleChangeModal()" style="display:none;">
			<div id="propose-change-button" class="player-action-button" title="Report a subtitle error" aria-label="Report a subtitle error"></div>
		</a>
		<div id="subtitle-error-modal" class="modal hide fade">
		  <div class="modal-header">
		    <button type="button" class="close" data-dismiss="modal" aria-hidden="true">&times;</button>
		    <h5>Report a subtitle error</h3>
		  </div>
		  <div class="error-modal-body modal-body">
			  		<div class="alert alert-info" id="error-modal-alert" style="display:none;">  
					  <a class="close" data-dismiss="alert">×</a>  
					  <div id="error-modal-alert-content"></div>  
					</div>
		         <h5>Vote up a proposed change</h5>
		         
		         <% for(int i=0; i<changes.length; i++){%>
		         	<div class="submitted-changes-container" id="changes-<%=correl[i] %>">
		         		<%for(Change chang : changes[i]){%>
				         <div class="proposed-change">
					         <h5 class="zeta original-sub"><%=chang.getOriginalContent() %></h5>
					         <h5 class="zeta changed-sub"><%=chang.getChangedContent() %></h5>
					         <div class="vote-box" id="voteup-<%= chang.getId()%>" onclick="javascript:voteChange('<%= chang.getId()%>');">
					         	<div class="count-votes"><%= chang.getNrVotes() %></div>
					         	<i class="icon-thumbs-up icon-white"></i>
					         </div>
				         </div>
			         	<%	} %>
			         </div>
		         	<%} %>
		         
		         <h5>or submit your own</h5>
		         <div class="proposed-change">
		         	 <h5 class="zeta original-sub" id="user-sub-original"></h5>
			         <input id="subtitle-change-field" type="text" name="Proposed change" style="margin-bottom: 5px;">
			         <div class="vote-box" id="submit-suggestion">
			         	<div class="count-votes">0</div>
			         	<i class="icon-thumbs-up icon-white"></i>
			         </div>
		         </div>
		  </div>
		</div>
		
		<% if(user.getNickname().equals(SettingsContainer.getAdministratorNickname())){ %>
		<a class="playerActionTab editButton" href="#edit-media-modal" data-toggle="modal">
			<div id="edit-meta-button" class="player-action-button" title="Edit metadata" aria-label="Edit metadata"></div>
		</a>
		<div id="edit-media-modal" class="modal hide fade">
		  <div class="modal-header">
		    <button type="button" class="close" data-dismiss="modal" aria-hidden="true">&times;</button>
		    <h5>Edit media</h3>
		  </div>
		  <div class="modal-body">
		         <iframe id="<%=item.getId() %>ifrm" src="EditMedia?media=<%=item.getId() %>" seamless frameborder="0" scrolling="no" onload='javascript:resizeIframe("<%=item.getId() %>");'></iframe>
		  </div>
		</div>
		<% } %>
	</div>
	<% } %>
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