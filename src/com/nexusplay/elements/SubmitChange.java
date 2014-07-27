package com.nexusplay.elements;

import java.io.IOException;
import java.io.PrintWriter;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.Change;
import com.nexusplay.containers.User;
import com.nexusplay.db.ChangesDatabase;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class SubmitChange
 */
@WebServlet(description = "Used for submitting a new change proposal via AJAX", urlPatterns = { "/SubmitChange" })
public class SubmitChange extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public SubmitChange() {
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
        		
        		Change[] assocChanges = ChangesDatabase.getAssociatedChanges(request.getParameter("subID"));
            	for(Change item : assocChanges){
            		if(item.getVotes().contains(userID)){
            			out.println("You've already voted!");
            			return;
            		}
            	}
			} catch (Exception e) {
				request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
				request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
				request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
				e.printStackTrace();
				return;
			}
        }
        
        
        Change chg = new Change(request.getParameter("changedContent"), request.getParameter("originalContent"), request.getParameter("subID"), userID + ";");
        try {
			ChangesDatabase.pushChange(chg);
			out.println("Change submitted!");
		} catch (Exception e) {
			out.println(e.getMessage());
			e.printStackTrace();
		}
        out.close();
	}

}
