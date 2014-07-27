package com.nexusplay.containers;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileNotFoundException;
import java.io.FileOutputStream;
import java.io.IOException;
import java.math.BigInteger;
import java.sql.SQLException;

import org.apache.commons.io.IOUtils;

import com.nexusplay.db.SubtitlesDatabase;
import com.nexusplay.security.RandomContainer;

/**
 * Contains a proposed change (to a subtitle).
 * @author alex
 *
 */
public class Change {

	private String targetID, changedContent, id, originalContent, votes;
	private int nrVotes;
	
	/**
	 * Constructor for creating new objects, prior to storing them in the database.
	 * @param changedContent The change's original data
	 * @param originalContent The change's new data
	 * @param targetID The object targeted by the change
	 * @param votes The user IDs that voted this change
	 */
	public Change(String changedContent, String originalContent, String targetID, String votes){
		this.changedContent = changedContent;
		this.originalContent = originalContent;
		this.targetID = targetID;
		this.votes = votes;
		nrVotes = votes.length() - votes.replace(";", "").length();
		generateId();
	}
	
	/**
	 * This constructor should only be used for recreating a stored object.
	 * @param changedContent The change's original data
	 * @param originalContent The change's new data
	 * @param targetID The object targeted by the change
	 * @param votes The user IDs that voted this change
	 * @param id The change's unique ID
	 */
	public Change(String changedContent, String originalContent, String targetID, String votes, String id){
		this.changedContent = changedContent;
		this.originalContent = originalContent;
		this.targetID = targetID;
		this.votes = votes;
		nrVotes = votes.length() - votes.replace(";", "").length();
		this.id = id;
	}
	
	/**
	 * Commits a change to disk.
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws FileNotFoundException Thrown if we're denied access to the subtitle file
	 * @throws IOException Thrown if an error appears while writing the file
	 */
	public void commitChange() throws SQLException, FileNotFoundException, IOException{
		Subtitle sub = SubtitlesDatabase.getSubtitleByID(targetID);
		FileInputStream input = new FileInputStream(SettingsContainer.getAbsoluteSubtitlePath() + File.separator + sub.getId() + ".vtt");
		String content = IOUtils.toString(input, "UTF-8");
		content = content.replaceAll(originalContent, changedContent);
		content = content.replaceAll(originalContent.replaceAll("\n", "\r\n"), changedContent.replaceAll("\n", "\r\n"));
		FileOutputStream output = new FileOutputStream(SettingsContainer.getAbsoluteSubtitlePath() + File.separator + sub.getId() + ".vtt");
		IOUtils.write(content, output, "UTF-8");
		output.close();
		input.close();
		
	}
	
	/**
     * Generates a new unique ID for the item
     */
	public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }

	/**
	 * @return The ID of the Media element associated to this object
	 */
	public String getTargetID() {
		return targetID;
	}

	/**
	 * @param targetID The new ID of the Media element associated to this object
	 */
	public void setTargetID(String targetID) {
		this.targetID = targetID;
	}

	/**
	 * @return The change itself
	 */
	public String getChangedContent() {
		return changedContent;
	}

	/**
	 * @param content The new data to change
	 */
	public void setChangedContent(String content) {
		this.changedContent = content;
	}

	/**
	 * @return The change's unique ID
	 */
	public String getId() {
		return id;
	}

	/**
	 * @param id The change's new unique ID
	 */
	public void setId(String id) {
		this.id = id;
	}

	/**
	 * @return The user IDs who voted for this change
	 */
	public String getVotes() {
		return votes;
	}

	/**
	 * @param votes The new user IDs who voted for this change
	 */
	public void setVotes(String votes) {
		this.votes = votes;
		nrVotes = votes.length() - votes.replace(";", "").length();
	}

	/**
	 * @return The original content prior to changing
	 */
	public String getOriginalContent() {
		return originalContent;
	}

	/**
	 * @param originalContent The new original content prior to changing
	 */
	public void setOriginalContent(String originalContent) {
		this.originalContent = originalContent;
	}

	/**
	 * @return the nrVotes
	 */
	public int getNrVotes() {
		return nrVotes;
	}

	/**
	 * @param nrVotes the nrVotes to set
	 */
	public void setNrVotes(int nrVotes) {
		this.nrVotes = nrVotes;
	}
}
