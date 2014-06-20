package com.nexusplay.elements;

import java.io.File;
import java.io.IOException;
import java.io.PrintWriter;
import java.sql.SQLException;
import java.util.Iterator;
import java.util.List;

import javax.servlet.ServletException;
import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.apache.commons.fileupload.FileItem;
import org.apache.commons.fileupload.FileItemFactory;
import org.apache.commons.fileupload.FileUploadException;
import org.apache.commons.fileupload.disk.DiskFileItemFactory;
import org.apache.commons.fileupload.servlet.ServletFileUpload;

import com.nexusplay.containers.Collection;
import com.nexusplay.containers.Media;
import com.nexusplay.containers.SettingsContainer;
import com.nexusplay.db.CollectionsDatabase;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.security.RandomContainer;

/**
 * Servlet implementation class UploadPoster
 */
@WebServlet("/UploadPoster")
public class UploadPoster extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public UploadPoster() {
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
		boolean isMultipart = ServletFileUpload.isMultipartContent(request);
		 
        if (isMultipart) {
        	// Create a factory for disk-based file items
        	FileItemFactory factory = new DiskFileItemFactory();

        	// Create a new file upload handler
        	ServletFileUpload upload = new ServletFileUpload(factory);
        	Media media = null; Collection coll = null; String id = request.getParameter("id");;
        	if(request.getParameter("type").equals("media")){
            	try {
    				media = MediaDatabase.getMediaById(id);
    			} catch (SQLException e1) {
    				// TODO Auto-generated catch block
    				e1.printStackTrace();
    			}
        	}else{
        		try {
					coll = CollectionsDatabase.getCollectionById(id);
				} catch (SQLException e) {
					// TODO Auto-generated catch block
					e.printStackTrace();
				}
        	}
            try {
            	// Parse the request
            	List<FileItem> items = upload.parseRequest(request);
                Iterator<FileItem> iterator = items.iterator();
                while (iterator.hasNext()) {
                    FileItem item = (FileItem) iterator.next();
                    if (!item.isFormField()) {
                        String fileName = item.getName();	 
                        File path = new File(SettingsContainer.getAbsolutePosterPath());
                        long randId; File uploadedFile;
                        do{
                        randId = RandomContainer.getRandom().nextLong();
                        uploadedFile = new File(path + "/" + randId + fileName.substring(fileName.lastIndexOf(".")));
                        }while(uploadedFile.exists());
                        item.write(uploadedFile);
                        if(request.getParameter("type").equals("media")){
	                        media.setPoster(SettingsContainer.getPosterSource()+"/"+randId + fileName.substring(fileName.lastIndexOf(".")));
	                        MediaDatabase.replaceMedia(media);
	                        out.write("{ \"poster\" : \"" + media.getPoster() + "\" }");
                        }else{
                        	coll.setPoster(SettingsContainer.getPosterSource()+"/"+randId + fileName.substring(fileName.lastIndexOf(".")));
                        	CollectionsDatabase.replaceCollection(coll);
                        	out.write("{ \"poster\" : \"" + coll.getPoster() + "\" }");
                        }
                    }
                }
                
            } catch (FileUploadException e) {
                e.printStackTrace();
            } catch (Exception e) {
                e.printStackTrace();
            }
        }
	}

}
