package com.nexusplay.containers;

import java.math.BigInteger;

import com.nexusplay.security.RandomContainer;

/**
 * Contains a subtitle associated to a Media item.
 * @author alex
 *
 */
public class Subtitle {
	private String mediaID, language, id, filename;
	
	/**
	 * This constructor should only be used for recreating a stored object.
	 * @param mediaID The ID of the Media object this subtitle is linked to
	 * @param language The language of the translation
	 * @param id This subtitle's id
	 * @param filename The file path
	 */
	public Subtitle(String mediaID, String language, String id, String filename){
		this.mediaID = mediaID;
		this.language = language;
		this.id = id;
		this.filename = filename;
		
	}
	
	/**
	 * Constructor for creating new objects, prior to storing them in the database.
	 * @param mediaID The ID of the Media object this subtitle is linked to
	 * @param language The language of the translation
	 * @param filename The file path
	 */
	public Subtitle(String mediaID, String filename, String language){
		this.mediaID = mediaID;
		this.language = language;
		this.filename = filename;
		generateId();
	}
	
	/**
	 * Gets "srclang" HTML tag
	 * @return The String corresponding to the HTML tag
	 */
	public String getSourceLanguage(){
		return language.substring(0,2).toLowerCase();
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
	public String getMediaID() {
		return mediaID;
	}

	/**
	 * @param mediaID The ID of the new Media element associated to this object
	 */
	public void setMediaID(String mediaID) {
		this.mediaID = mediaID;
	}

	/**
	 * @return The translation's language
	 */
	public String getLanguage() {
		return language;
	}

	/**
	 * @param language The translation's new language
	 */
	public void setLanguage(String language) {
		this.language = language;
	}

	/**
	 * @return The object's unique ID
	 */
	public String getId() {
		return id;
	}

	/**
	 * @param id The object's new unique ID
	 */
	public void setId(String id) {
		this.id = id;
	}

	/**
	 * @return The file path
	 */
	public String getFilename() {
		return filename;
	}

	/**
	 * @param filename The new file path
	 */
	public void setFilename(String filename) {
		this.filename = filename;
	}
}
