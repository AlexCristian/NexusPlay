<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8" import="com.nexusplay.containers.Media,com.nexusplay.containers.User,com.nexusplay.db.MediaDatabase"%>
<script type="text/javascript">
	$(document).ready(function(){
		$('.menu-button').click(function (e) {
			  $(".controlContent").hide();
			  $("#" + $(this).data("target")).css("display","inline-block");
			});
		$(".publish").click(function() {
			var id = $(this).attr('id');
			var name = $("#"+id+"n").val();
			var category = $("#"+id+"c").val();
			var year = $("#"+id+"y").val();
			var collection = $("#"+id+"cll").val();
			var season = $("#"+id+"sn").val();
			var episode = $("#"+id+"ep").val();
			var dataString = 'name='+ name + '&id=' + id + '&category=' + category + '&year=' + year + '&collection=' + collection + '&season=' + season + '&episode=' + episode;
			$.ajax({
				type: "POST",
				url: "./PublishMedia",
				data: dataString,
				success: function(){
					$('#'+id+"hold").fadeOut();
				}
			});
		});
	});
	function createUploader(nr,id) {
        var uploader = new qq.FineUploader({
          // Pass the HTML element here
          element: document.getElementById(nr),
          // or, if using jQuery
          // element: $('#fine-uploader')[0],
          // Use the relevant server script url here
          // if it's different from the default “/server/upload”
          request: {
            endpoint: './UploadPoster?type=media&id='+id
          },
          text: {
              uploadButton: 'Upload poster'
            }
        });
      }
	function searchCollections(field){
		$.ajax({
			type: "POST",
			url: "./CollectionSearch",
			data: "q="+field.val()
		}).done(function ( data ) {
			return data.split("\n");
		});
	}
    
      window.onload = createUploader;
</script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.fineuploader-3.5.0.min.js'></script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/iframe.xss.response-3.5.0.js'></script>
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
		int i=0;
		for(Media item : unpublished){
			i++;
		%>	
			<div class="well uncategHolder" id="<%= item.getId() %>hold">
				<h5><%= item.getFilename().substring(1) %></h5>
				<p><b><i>Suggested metadata</i></b></p>
				<div class="adminPosterHolder">
					<div id="<%= i %>upload"></div>
					<script type="text/javascript">
						$(document).ready(function(){
							createUploader("<%= i %>upload","<%= item.getId() %>");
						});
					</script>
					<img style="display: inline-table; width:135px;" src=".<%= item.getPoster() %>"/>
					<a id="<%= item.getId() %>" class="publish submitButton pull-right topBarButton normalTopBarButton" style="color:white; width: 95px;">Publish</a>
				</div>
				<table class="adminTable">
					<tr>
						<td>Name</td>
						<td><input id="<%= item.getId() %>n" type="text" name="Name" placeholder="Name" style="margin-bottom:10px; margin-left:10px;" value="<%= item.getName() %>"></td>
					</tr>
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
						<td><input id="<%= i %>" type="checkbox" name="Collectible" style="margin-bottom:10px; margin-left:10px;"></td>
					</tr>
					<tr style="display:none;" class="<%= i %>h">
						<td>Collection</td>
						<script>
						$(document).ready(function(){
							$("#<%= item.getId() %>cll").typeahead({source:searchCollections($("#<%= item.getId() %>cll"))});
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
			</div>
		<%
		}
	%>
	<script type="text/javascript">
	$(document).ready(function(){
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
</div>
<div class="controlContent" id="collections">
	<h2>Manage collections</h2>
	<hr class="featurette-divider">
	<div class="well uncategHolder" style="text-align: center;">
	<p><b><i>Add a collection</i></b></p>
		<table class="adminTable">
			<tr>
				<td>Name</td>
				<td><input id="newcollnm" type="text" name="Name" placeholder="Name" style="margin-bottom:10px; margin-left:10px;"></td>
			</tr>
			<tr>
				<td>Year</td>
				<td><input id="newcollyr" type="text" name="Year" placeholder="Year" style="margin-bottom:10px; margin-left:10px;"></td>
			</tr>
		</table>
		<style>
			.qq-uploader{
				display:none;
				}
		</style>
		<a id="collectionSend" class="publish submitButton pull-right topBarButton normalTopBarButton" style="color:white; width: 100px;">Submit and select poster</a>
		<div id="collectionSendHidden" style="display:none;"></div>
		<script type="text/javascript">
						$(document).ready(function(){
							var uploadParams = {};
							var uploader = new qq.FineUploader({
						          // Pass the HTML element here
						          element: document.getElementById('collectionSend'),
						          // or, if using jQuery
						          // element: $('#fine-uploader')[0],
						          // Use the relevant server script url here
						          // if it's different from the default “/server/upload”
						          request: {
						            endpoint: "./AddCollection",
						            params: uploadParams
						          },
						          text: {
						              uploadButton: 'Submit and select poster'
						            }
						        });
							$("#collectionSend").click(function(){
								uploadParams['name'] = $("#newcollnm").val();
				            	uploadParams['year'] = $("#newcollyr").val();
								$("#collectionSendHidden").fineUploader('setParams', uploadParams );
								$("#collectionSendHidden").click();
							});
						});
					</script>
	</div>
</div>