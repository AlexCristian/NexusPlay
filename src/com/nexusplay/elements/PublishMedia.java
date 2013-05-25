package com.nexusplay.elements;

import java.io.IOException;
import java.sql.SQLException;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import com.nexusplay.containers.Media;
import com.nexusplay.db.CollectionsDatabase;
import com.nexusplay.db.MediaDatabase;

/**
 * Servlet implementation class PublishMedia
 */
@WebServlet("/PublishMedia")
public class PublishMedia extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public PublishMedia() {
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
		Media media = null;
		try {
			media = MediaDatabase.getMediaById(request.getParameter("id"));
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
		media.setCategory(request.getParameter("category"));
		media.setEpisode(Integer.parseInt(request.getParameter("episode")));
		media.setSeason(Integer.parseInt(request.getParameter("season")));
		media.setName(request.getParameter("name"));
		media.setPublished(1);
		media.setYear(request.getParameter("year"));
		try {
			media.setCollectionID(CollectionsDatabase.getCollectionByName(request.getParameter("collection")).getID());
		} catch (Exception e1) {
			// TODO Auto-generated catch block
			e1.printStackTrace();
		}
		try {
			MediaDatabase.replaceMedia(media);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}

}
