package com.nexusplay.elements;

import java.io.IOException;
import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.User;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class AddRemoveSubscription
 */
@WebServlet("/AddRemoveSubscription")
public class AddRemoveSubscription extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public AddRemoveSubscription() {
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
		User user = null;
		try {
			user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
		} catch (Exception e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
		if(user!=null){
			if(user.getSubscriptions().contains(request.getAttribute("id"))){
				user.unsubscribe(request.getParameter("id"));
			}else{
				user.subscribe(request.getParameter("id"));
			}
		}
	}

}
