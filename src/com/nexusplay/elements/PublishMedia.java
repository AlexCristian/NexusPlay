package com.nexusplay.elements;

import java.io.File;
import java.io.IOException;
import java.sql.SQLException;
import java.util.ArrayList;
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
import com.nexusplay.containers.Subtitle;
import com.nexusplay.containers.User;
import com.nexusplay.db.CollectionsDatabase;
import com.nexusplay.db.MediaDatabase;
import com.nexusplay.db.SubtitlesDatabase;
import com.nexusplay.db.UsersDatabase;
import com.nexusplay.security.RandomContainer;

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
		response.setContentType("text/html");
		
		ArrayList<String> existingSubsIDs = new ArrayList<String>();
		User user = null;
		try {
			user = UsersDatabase.getUserById((String) request.getSession().getAttribute("userID"));
		} catch (Exception e1) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e1.printStackTrace();
			return;
		}
		
        if(!user.getNickname().equals(SettingsContainer.getAdministratorNickname())){
        	request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
        	request.getRequestDispatcher("/templates/information_screens/AccessDenied.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			return;
        }
        

		boolean isMultipartContent = ServletFileUpload.isMultipartContent(request);
		if (!isMultipartContent) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			return;
		}

		Media media = null;
		try {
			media = MediaDatabase.getMediaById(request.getParameter("id"));
		} catch (SQLException e) {
			e.printStackTrace();
		}
		media.setPublished(1);
		ArrayList<Subtitle> subs = new ArrayList<Subtitle>();
		FileItemFactory factory = new DiskFileItemFactory();
		ServletFileUpload upload = new ServletFileUpload(factory);
		try {
			List<FileItem> fields = upload.parseRequest(request);
			Iterator<FileItem> it = fields.iterator();
			if (!it.hasNext()) {
				request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
				request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
				request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
				return;
			}
			while (it.hasNext()) {
				FileItem fileItem = it.next();
				boolean isFormField = fileItem.isFormField();
				if (isFormField) {
					if(fileItem.getFieldName().equals("Category"))
						media.setCategory(fileItem.getString());
					else if(fileItem.getFieldName().equals("Episode"))
						media.setEpisode(Integer.parseInt(fileItem.getString()));
					else if(fileItem.getFieldName().equals("Season"))
						media.setSeason(Integer.parseInt(fileItem.getString()));
					else if(fileItem.getFieldName().equals("Name"))						
						media.setName(fileItem.getString());
					else if(fileItem.getFieldName().equals("Year"))
						media.setYear(fileItem.getString());
					else if(fileItem.getFieldName().contains("Subtitle language ")){
						int nrSub = Integer.parseInt(fileItem.getFieldName().replace("Subtitle language ", "")) -1;
						while(subs.size()<=nrSub){
							subs.add(null);
						}
						if(subs.get(nrSub) == null){
							subs.set(nrSub, new Subtitle(media.getId(), "", ""));
						}
						subs.get(nrSub).setLanguage(fileItem.getString());
					}else if(fileItem.getFieldName() == "collection"){
						try {
							Collection coll= null;
							coll = CollectionsDatabase.getCollectionByName(fileItem.getString());
							media.setCollectionID(coll.getID());
							media.setPoster(coll.getPoster());
						} catch (Exception e1) {
							//Not part of a collection
						}
					}else if(fileItem.getFieldName().contains("Existing ")){
						String id = fileItem.getFieldName().replace("Existing ", "");
						Subtitle sub = SubtitlesDatabase.getSubtitleByID(id);
						sub.setLanguage(fileItem.getString());
						SubtitlesDatabase.replaceSubtitle(sub);
						existingSubsIDs.add(id);
					}
						
				} else {
					if(fileItem.getFieldName().equals("poster") && !fileItem.getName().equals("")){
						
						long randId; File uploadedFile;
                        do{
	                        randId = RandomContainer.getRandom().nextLong();
	                        uploadedFile = new File(SettingsContainer.getAbsolutePosterPath() + "/" + randId + fileItem.getName().substring(fileItem.getName().lastIndexOf(".")));
                        }while(uploadedFile.exists());
                        try {
                        	File temp = new File(SettingsContainer.getAbsolutePosterPath());
                        	temp.mkdirs();
							fileItem.write(uploadedFile);
						} catch (Exception e) {
							request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
							request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
							request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
							e.printStackTrace();
							return;
						}
                        media.setPoster(SettingsContainer.getPosterSource()+"/"+randId + fileItem.getName().substring(fileItem.getName().lastIndexOf(".")));
					}else if(fileItem.getFieldName().contains("Subtitle file ") && !fileItem.getName().equals("")){
						if(!fileItem.getName().contains(".") || !fileItem.getName().substring(fileItem.getName().lastIndexOf(".")).equals(".vtt")){
							throw new FileUploadException("Invalid subtitle format");
						}
						
						int nrSub = Integer.parseInt(fileItem.getFieldName().replace("Subtitle file ", ""))-1;
						while(subs.size()<=nrSub){
							subs.add(null);
						}
						if(subs.get(nrSub) == null){
							subs.set(nrSub, new Subtitle(media.getId(), "", ""));
						}
						File uploadedFile = new File(SettingsContainer.getAbsoluteSubtitlePath() + "/" + subs.get(nrSub).getId() + fileItem.getName().substring(fileItem.getName().lastIndexOf(".")));
						try {
							File temp = new File(SettingsContainer.getAbsoluteSubtitlePath());
                        	temp.mkdirs();
							fileItem.write(uploadedFile);
						} catch (Exception e) {
							request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
							request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
							request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
							e.printStackTrace();
							return;
						}
						subs.get(nrSub).setFilename(SettingsContainer.getSubtitleSource()+"/"+ subs.get(nrSub).getId() + fileItem.getName().substring(fileItem.getName().lastIndexOf(".")));
					}
						
				}
			}
		} catch (FileUploadException e) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e.printStackTrace();
			return;
		} catch (Exception e){
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InternalError.jsp").include(request, response);
			request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
			e.printStackTrace();
			return;
		}
		
		try {
			Subtitle[] storedSubs = SubtitlesDatabase.getAssociatedSubtitles(media.getId());
			for(Subtitle sub : storedSubs){
				if(!existingSubsIDs.contains(sub.getId())){
					SubtitlesDatabase.deleteSubtitle(sub.getId());
				}
			}
			
			MediaDatabase.replaceMedia(media);
			MediaDatabase.propagateMediaNotification(media);
			for(Subtitle item : subs){
				if(!item.getFilename().equals("") && !item.getLanguage().equals("")){
					SubtitlesDatabase.pushSubtitle(item);
				}
			}
		} catch (SQLException e) {
			request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
			request.getRequestDispatcher("/templates/information_screens/InvalidParameters.jsp").include(request, response);
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
		request.getRequestDispatcher("/templates/elements/MinimalHeader.jsp").include(request, response);
		request.getRequestDispatcher("/templates/information_screens/Success.jsp").include(request, response);
		request.getRequestDispatcher("/templates/elements/MinimalFooter.jsp").include(request, response);
	}

}
