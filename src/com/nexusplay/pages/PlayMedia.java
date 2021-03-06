package com.nexusplay.pages;

import com.nexusplay.containers.Change;
import com.nexusplay.containers.Media;
import com.nexusplay.containers.Subtitle;
import com.nexusplay.containers.User;
import com.nexusplay.db.ChangesDatabase;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.db.SubtitlesDatabase;
import com.nexusplay.db.UsersDatabase;

import java.io.IOException;
import java.sql.SQLException;

import javax.servlet.ServletException;
import javax.servlet.http.*;

public class PlayMedia extends HttpServlet
{

    public PlayMedia()
    {
    }

    protected void doGet(HttpServletRequest request, HttpServletResponse response)
        throws ServletException, IOException
    {
        response.setContentType("text/html");
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
        request.getRequestDispatcher("/templates/elements/Header.jsp").include(request, response);
        Media item = null;
        try
        {
            item = MediaDatabase.getMediaById(request.getParameter("w"));
            if(item==null){
            	throw new Exception("Media not found exception");
            }
            MediaDatabase.incrementViews(item.getId());
            request.setAttribute("media", item);
            if(userID!=null){
            	UsersDatabase.setMediaWatched(userID, item.getId());
            }
            Subtitle[] subs = SubtitlesDatabase.getAssociatedSubtitles(item.getId());
            request.setAttribute("subs", subs);
            Change[][] changes = new Change[subs.length][];
            String[] changeCorrelation = new String[subs.length];
            for(int i=0; i<subs.length; i++){
            	changes[i] = ChangesDatabase.getAssociatedChanges(subs[i].getId());
            	changeCorrelation[i] = subs[i].getSourceLanguage();
            }
            request.setAttribute("changes", changes);
            request.setAttribute("changes-correlation", changeCorrelation);
            request.getRequestDispatcher("/templates/PlayMedia.jsp").include(request, response);
        }
        catch(SQLException e)
        {
        	request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
            e.printStackTrace();
        } catch (Exception e) {
        	request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
			e.printStackTrace();
		}

        request.getRequestDispatcher("/templates/elements/Footer.jsp").include(request, response);
    }

    protected void doPost(HttpServletRequest httpservletrequest, HttpServletResponse httpservletresponse)
        throws ServletException, IOException
    {
    }

    private static final long serialVersionUID = 1L;
}
