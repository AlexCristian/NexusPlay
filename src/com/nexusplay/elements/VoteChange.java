package com.nexusplay.elements;

import java.io.IOException;
import java.io.PrintWriter;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.Change;
import com.nexusplay.containers.SettingsContainer;
import com.nexusplay.containers.User;
import com.nexusplay.db.ChangesDatabase;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class VoteChange
 */
@WebServlet(description = "Increments votes on a change", urlPatterns = { "/VoteChange" })
public class VoteChange extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public VoteChange() {
        super();
        // TODO Auto-generated constructor stub
    }

	/**
	 * @see HttpServlet#doGet(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		// TODO Auto-generated method stub
	}

	/**
	 * @see HttpServlet#doPost(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doPost(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		response.setContentType("text/html");
        PrintWriter out = response.getWriter();
        
        String userID=null;
        
        if(request.getSession().getAttribute("userID")!=null){
        	try {
        		User user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
				userID = user.getId();
        		request.setAttribute("user", user);
			} catch (Exception e) {
				request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
				request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
				request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
				e.printStackTrace();
				return;
			}
        }
        
        try {
        	String changeID = request.getParameter("changeId");
        	Change chng = ChangesDatabase.getChangeByID(changeID);
        	Change[] assocChanges = ChangesDatabase.getAssociatedChanges(request.getParameter("subId"));
        	for(Change item : assocChanges){
        		if(item.getVotes().contains(userID)){
        			out.println("You've already voted!");
        			return;
        		}
        	}
        	
        	if(chng.getNrVotes() + 1 >= SettingsContainer.getVotesRequired()){
        		chng.commitChange();
        		String lookFor = chng.getOriginalContent();
        		for(Change item : assocChanges){
        			if(item.getOriginalContent().equals(lookFor)){
        				ChangesDatabase.deleteChange(item.getId());
        			}
        		}
        		out.println("Change applied!");
        	}else{
				ChangesDatabase.voteUpChange(changeID, userID);
				out.println("Vote submitted!");
        	}
		} catch (Exception e) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e.printStackTrace();
			return;
		}
        out.close();
	}

}
