<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8" import="java.util.ArrayList,com.nexusplay.containers.Collection,com.nexusplay.containers.Media"%>
<% 
	Collection coll = (Collection) request.getAttribute("collection");
	String seasons = coll.getSeasons() + ((coll.getSeasons()>1) ? " Seasons" : " Season");
%>
<div class="modal-header collectionHeader">
    <button style="margin:10px" type="button" class="close" data-dismiss="modal" aria-hidden="true">&times;</button>
    <div style="display: inline-block;">
		<div class="poster-wrapper">
			<img id="moviePoster" src=".<%= coll.getPoster() %>" style="width:140px;"/>
       		<div class="ribbon-wrapper"><div class="ribbon ribbon-blue"></div></div>
		</div>
    </div>
    <div style="margin:20px; position: relative; top: -30%; display: inline-table;">
    	<h3><%= coll.getName() %></h3>
    	<h5><%= seasons %>, <%= coll.getYear() %></h5>
    </div>
    <div class='seasonsSlider'>
	    <div class='seasonsHolder'>
	    	<% 
	    		ArrayList<ArrayList<Media>> episodes = coll.getEpisodes();
	    		boolean first=true; int x=0;
	    		for(int i=1; i<=coll.getSeasons(); i++){
	    			while(i<coll.getSeasons()&&episodes.get(i).isEmpty())
	    				i++;
	    			if(episodes.get(i).isEmpty())
	    				continue;
	    			x++;
	    	%>
		    <div id='collses<%=x %>' class='seasonSelector'>
	    		<h5 <%= (first) ? "style='color:white;'" : ""%> id='collses<%=x %>' class='seasonSelector'>Season <%=i %></h5>
		    </div>
		    <% 
		    	first=false;
		    	} 
		    %>
	    </div>
	</div>
</div>
<div class="modal-body">
	<%
		first=true; x=0;
		for(int i=1; i<episodes.size(); i++){
			while(episodes.get(i).isEmpty()&&i<coll.getSeasons())
		i++;
			if(episodes.get(i).isEmpty())
				continue;
			x++;
	%>
	<div id="collsescnt<%=x %>" class="seasonContent" <% if(first){ %> style="display:block;" <% } %>>
		<table class="table table-bordered table-hover">
			<tr>
				<td>Episode</td>
				<td>Title</td>
			</tr>
			<%
				for(Media item : episodes.get(i)){
			%>
			<tr style="cursor:pointer" onclick="document.location.href='./watch?w=<%= item.getId() %>'">
				<td><div class="episode"><%=item.getEpisode() %></div></td>
				<td><%=item.getName() %></td>
			</tr>
			<%
				first=false;
				}
			%>
		</table>
     </div>
     <%
		}
     %>
     
</div>