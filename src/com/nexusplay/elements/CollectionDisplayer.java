package com.nexusplay.elements;

import java.io.IOException;
import java.sql.SQLException;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.Collection;
import com.nexusplay.containers.User;
import com.nexusplay.db.CollectionsDatabase;
import com.nexusplay.db.UsersDatabase;

/**
 * Servlet implementation class CollectionDisplayer
 */
@WebServlet("/CollectionDisplayer")
public class CollectionDisplayer extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public CollectionDisplayer() {
        super();
        // TODO Auto-generated constructor stub
    }

	/**
	 * @see HttpServlet#doGet(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doGet(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		doPost(request, response);
	}

	/**
	 * @see HttpServlet#doPost(HttpServletRequest request, HttpServletResponse response)
	 */
	protected void doPost(HttpServletRequest request, HttpServletResponse response) throws ServletException, IOException {
		Collection coll = null; User user = null;
		try {
			coll = CollectionsDatabase.getCollectionById(request.getParameter("id"));
			if(request.getSession().getAttribute("userID") instanceof String){
				user = UsersDatabase.getUserById((String)request.getSession().getAttribute("userID"));
				request.setAttribute("user", user);
			}
		} catch (SQLException e) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e.printStackTrace();
			return;
		} catch (Exception e) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e.printStackTrace();
			return;
		}
		request.setAttribute("collection", coll);
        request.getRequestDispatcher("/templates/elements/CollectionDisplayer.jsp").include(request, response);
	}

}
