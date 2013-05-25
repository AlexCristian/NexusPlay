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
 * Servlet implementation class StorePlaybackState
 */
@WebServlet("/StorePlaybackState")
public class StorePlaybackState extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public StorePlaybackState() {
        super();
        // TODO Auto-generated constructor stub
    }

	/**
	 * @see HttpServlet#doGet(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		User user = null;
		try {
			user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
		} catch (Exception e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
		if(user!=null){
			if(request.getParameter("time").equals("0"))
				user.popPausedState(request.getParameter("id"));
			else
				user.storePausedState(request.getParameter("id"),request.getParameter("time"));
		}
	}

	/**
	 * @see HttpServlet#doPost(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doPost(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		// TODO Auto-generated method stub
	}

}
