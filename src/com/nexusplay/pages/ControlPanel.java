package com.nexusplay.pages;

import java.io.IOException;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.SettingsContainer;
import com.nexusplay.containers.User;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class ControlPanel
 */
@WebServlet("/ControlPanel")
public class ControlPanel extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public ControlPanel() {
        super();
        // TODO Auto-generated constructor stub
    }

	/**
	 * @see HttpServlet#doGet(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		response.setContentType("text/html");
        User user = null;
		try {
			user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
			request.setAttribute("user", user);
		} catch (Exception e1) {
			// TODO Auto-generated catch block
			e1.printStackTrace();
		}
		request.getRequestDispatcher("/templates/elements/Header.jsp").include(request, response);
		
        if(request.getSession().getAttribute("userID")!=null && user.getNickname().equals(SettingsContainer.getAdministratorNickname())){
			request.getRequestDispatcher("/templates/ControlPanel.jsp").include(request, response);
        }else{
        	request.getRequestDispatcher("/templates/information_screens/AccessDenied.jsp").include(request, response);
        }
        request.getRequestDispatcher("/templates/elements/Footer.jsp").include(request, response);
	}

	/**
	 * @see HttpServlet#doPost(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doPost(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		// TODO Auto-generated method stub
	}

}
