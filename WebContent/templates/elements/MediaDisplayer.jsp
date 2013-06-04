<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8" import="com.nexusplay.containers.*,java.util.*,com.nexusplay.db.CollectionsDatabase,com.nexusplay.db.UsersDatabase"%>
<%
	CategoryContainer[] totalCategs = (CategoryContainer[]) pageContext.findAttribute("categories");
	request.removeAttribute("categories");
	String randomToken = String.valueOf((new Random()).nextInt(0x3b9ac9ff));
	User user = null;
	if(request.getSession().getAttribute("userID") instanceof String){
		user = UsersDatabase.getUserById((String)request.getSession().getAttribute("userID"));
	}
	%>
	<div class='categoriesHolder'>
	<%	
	ArrayList ids = new ArrayList();
	for(int i = 0; i < totalCategs.length; i++)
	{
	    String currentID = totalCategs[i].getCategoryName().toLowerCase().replaceAll(" ", "") + randomToken;
	    ids.add(currentID);
	    %>
	    <div id='<%=currentID %>' class='categorySelector'<%=(i != 0) ? "" : "style='color:black;'" %>>
	    	<%=totalCategs[i].getCategoryName() %>
	    </div>
	    <%
	}
	%>
	</div>
	<div class='mediaHolder'>
	<%
	for(int i = 0; i < totalCategs.length; i++)
	{
		%>
	    <div id='resultPage<%=(i + 1) %>' <%=(i!=0) ? "style='display:none;'" : "" %>>
	    <%
	    for(Iterator iterator1 = totalCategs[i].getMedia().iterator(); iterator1.hasNext();)
	    {
	        Media item = (Media)iterator1.next();
	        %>
	        <a class='videoLink' <% if(item.getCollectionID().contentEquals("")){ %>href='<%=request.getContextPath() %>/watch?w=<%=item.getId() %>'<% }else{ %>style="cursor:pointer" onClick="displayCollection('<%=item.getCollectionID() %>')" <% } %>>
	        	<div class='mediaTile'>
	        		<img id='moviePoster' src='<%=request.getContextPath()+item.getPoster() %>'/>
	        		<p><%=(item.getCollectionID().contentEquals("")) ? item.getName() : CollectionsDatabase.matchIdWithName(item.getCollectionID()) %>
	        			<br>
	        			<% if(item.getCollectionID().contentEquals("")&&user!=null&&user.isWatched(item.getId())){ %>
	        				<i class="icon-ok  icon-white"></i> <% } %>
	        			<%=item.getYear() %>
	        		</p>
	        	</div>
	        </a>
	        <%
	    }
		%>
	    </div>
	    <%
	}
	%>
	</div>
	<script type='text/javascript'>
		var keyCorrespondence = {}; 
		onDisplay=1;
	<%
	for(int i = 0; i < ids.size(); i++)
	{
		%>
	     keyCorrespondence['<%=(String)ids.get(i) %>'] = <%=i %>;
	     keyCorrespondence[<%=(i + 1) %>] = '<%=(String)ids.get(i) %>';
	    $('#<%=(String)ids.get(i) %>').on('click', function(event){
	    	$('#'+keyCorrespondence[onDisplay]).css({
	    		'color':'rgb(160, 160, 160)'
	    		});
	    	$('#resultPage'+onDisplay).fadeOut(200, function(){
	    		$('#resultPage'+onDisplay).fadeIn(200);
	    		});
	    	onDisplay = <%=(i + 1) %>;
	    	$('#'+keyCorrespondence[onDisplay]).css({
	    		'color':'black'});
	    	});
		<%
	}
	totalCategs=null; ids=null; randomToken = null;
	%>
	</script>