package com.nexusplay.elements;

import java.io.IOException;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.Media;
import com.nexusplay.containers.SettingsContainer;
import com.nexusplay.containers.User;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class EditMedia
 */
@WebServlet("/EditMedia")
public class EditMedia extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public EditMedia() {
        super();
        // TODO Auto-generated constructor stub
    }

	/**
	 * @see HttpServlet#doGet(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		response.setContentType("text/html");
        User user = null;
        Media item = null;
		try {
			user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
			request.setAttribute("user", user);
			item = MediaDatabase.getMediaById(request.getParameter("media"));
			request.setAttribute("media", item);
		} catch (Exception e1) {
			// TODO Auto-generated catch block
			e1.printStackTrace();
		}
		
        if(request.getSession().getAttribute("userID")!=null && user.getNickname().equals(SettingsContainer.getAdministratorNickname())){
			request.getRequestDispatcher("/templates/EditMedia.jsp").include(request, response);
        }else{
        	request.getRequestDispatcher("/templates/exceptions/Error.jsp").include(request, response);
        }
	}

	/**
	 * @see HttpServlet#doPost(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doPost(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		// TODO Auto-generated method stub
	}

}
