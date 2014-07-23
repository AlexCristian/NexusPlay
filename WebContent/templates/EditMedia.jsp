<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8" import="com.nexusplay.containers.Media, com.nexusplay.containers.Subtitle"%>
<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">
<html>
<head>
		<title>Alex's Nexus</title>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.js'></script>		
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/bootstrap.js'></script>
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap.min.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap-responsive.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/typeplate-unminified.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/default.css' />
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
		i = 1;
		$('#add-sub').click(function (){
			newRow = "<tr>";
			newRow += "<td class='addsub-table'><button type='button' class='close' aria-hidden='true' onclick=\"javascript:deleteRow('" + i + "');\">×</button></td>";
			newRow += "<td class='language-table'><input style='width: 135px;' type='text' name='Subtitle language " + i + "' placeholder=\"Subtitle's language\"></td>";
			newRow += "<td class='file-table'><input type='file' size='50' name='Subtitle file " + i + "'></td>";
			newRow += "</tr>";
			console.log(newRow);
			$('#substable > tbody:last').append(newRow);
			i++;
		});
	});
</script>
<script type='text/javascript' src='<%=request.getContextPath()%>/js/iframe.xss.response-3.5.0.js'></script>

	<%
		Media item = (Media) request.getAttribute("media");
		Subtitle[] subs = (Subtitle[]) request.getAttribute("subtitles");
		int i = 1, j=1;
	%>
	<div class="well uncategHolder" id="<%= item.getId() %>hold">
		<h5><%= item.getFilename() %></h5>
		<p><b><i>Suggested metadata</i></b></p>
		<form name="PublishMedia" action="PublishMedia?id=<%= item.getId() %>" method="post" enctype="multipart/form-data">
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
					<td><input id="<%= i %>" type="checkbox" name="Collectible" style="margin-bottom:10px; margin-left:10px;" onclick="javascript:parent.resizeIframe('<%=item.getId() %>');"></td>
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
				<tr>
					<td colspan="2">
						<table id="substable" class="table-bordered" style="width:100%;">
							<thead class="topBarButton normalTopBarButton">
								<tr>
									<th id="add-sub" class="addsub-table" onclick="javascript:parent.resizeIframe('<%=item.getId() %>');">+</th>
									<th class='language-table'>Language</th>
									<th class='file-table'>Source file</th>
								</tr>
							</thead>
							<tbody>
							<% for(Subtitle sub : subs){ %>
								<tr>
									<td class='addsub-table'><button type="button" class="close" aria-hidden="true" onclick="javascript:deleteSubtitle('<%=item.getId() %>');">×</button></td>
									<td class='language-table'><input style='width: 135px;' type='text' name='Existing <%=sub.getId() %>' placeholder="Subtitle's language" value="<%=sub.getLanguage() %>"></td>
									<td class='file-table'><a href=".<%=sub.getFilename() %>"><%=sub.getFilename() %></a></td>
								</tr>
							<% 
								j++;
								} %>
							</tbody>
						</table>
					</td>
				</tr>
			</table>
		</form>
	</div>

</body>
</html>