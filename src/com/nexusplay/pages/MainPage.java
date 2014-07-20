package com.nexusplay.pages;

import com.nexusplay.containers.User;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.db.UsersDatabase;
import com.nexusplay.elements.*;
import java.io.IOException;
import java.io.PrintWriter;
import java.sql.SQLException;
import javax.servlet.ServletException;
import javax.servlet.http.*;

public class MainPage extends HttpServlet
{

    public MainPage()
    {
    }

    protected void doGet(HttpServletRequest request, HttpServletResponse response)
        throws ServletException, IOException
    {
        response.setContentType("text/html");
        PrintWriter out = response.getWriter();
        
        if(request.getSession().getAttribute("userID")!=null){
        	try {
        		User user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
				request.setAttribute("user", user);
			} catch (Exception e) {
				request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
				e.printStackTrace();
			}
        }
        
        request.getRequestDispatcher("/templates/elements/Header.jsp").include(request, response);
        try
        {
            MediaDisplayer selector = new MediaDisplayer(MediaDatabase.getRecentMedia(10));
            request.setAttribute("categories", selector.getCategories());
            request.getRequestDispatcher("/templates/MainPage.jsp").include(request, response);
        }
        catch(SQLException e)
        {
        	request.getRequestDispatcher("/templates/exceptions/SQLError.jsp").include(request, response);
            e.printStackTrace();
        }
        request.getRequestDispatcher("/templates/elements/Footer.jsp").include(request, response);
        out.close();
    }

    protected void doPost(HttpServletRequest httpservletrequest, HttpServletResponse httpservletresponse)
        throws ServletException, IOException
    {
    }

    private static final long serialVersionUID = 1L;
}
