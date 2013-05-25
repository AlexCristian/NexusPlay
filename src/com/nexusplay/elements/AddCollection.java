package com.nexusplay.elements;

import java.io.File;
import java.io.IOException;
import java.io.OutputStream;
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
import org.apache.commons.fileupload.MultipartStream;
import org.apache.commons.fileupload.disk.DiskFileItemFactory;
import org.apache.commons.fileupload.servlet.ServletFileUpload;

import com.nexusplay.containers.Collection;
import com.nexusplay.containers.Media;
import com.nexusplay.containers.SettingsContainer;
import com.nexusplay.db.CollectionsDatabase;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.security.RandomContainer;

/**
 * Servlet implementation class AddCollection
 */
@WebServlet("/AddCollection")
public class AddCollection extends HttpServlet {
	private static final long serialVersionUID = 1L;
       
    /**
     * @see HttpServlet#HttpServlet()
     */
    public AddCollection() {
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
		Collection coll;
        if (isMultipart) {
        	// Create a factory for disk-based file items
        	FileItemFactory factory = new DiskFileItemFactory();

        	// Create a new file upload handler
        	ServletFileUpload upload = new ServletFileUpload(factory);
            try {
            	// Parse the request
            	List /* FileItem */ items = upload.parseRequest(request);
                Iterator iterator = items.iterator();
                while (iterator.hasNext()) {
                    FileItem item = (FileItem) iterator.next();
                    if (!item.isFormField()) {
                        String fileName = item.getName();	 
                        String root = getServletContext().getRealPath("/");
                        File path = new File(SettingsContainer.getAbsolutePosterPath());
                        if (!path.exists()) {
                            boolean status = path.mkdirs();
                        }
                        long randId; File uploadedFile;
                        do{
                        randId = RandomContainer.getRandom().nextLong();
                        uploadedFile = new File(path + "/" + randId + fileName.substring(fileName.lastIndexOf(".")));
                        }while(uploadedFile.exists());
                        item.write(uploadedFile);
                        coll = new Collection(request.getParameter("name"), request.getParameter("year"),SettingsContainer.getPosterSource()+"/"+randId + fileName.substring(fileName.lastIndexOf(".")));
                    	CollectionsDatabase.pushCollection(coll);
                        out.write("{ \"poster\" : \"" + coll.getPoster() + "\" }");
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
