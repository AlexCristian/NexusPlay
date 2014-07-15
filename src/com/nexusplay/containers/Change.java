package com.nexusplay.containers;

import java.math.BigInteger;

import com.nexusplay.security.RandomContainer;

/**
 * Contains a proposed change (to a subtitle).
 * @author alex
 *
 */
public class Change {

	private String targetID, changedContent, id, originalContent;
	private int votes;
	
	/**
	 * Constructor for creating new objects, prior to storing them in the database.
	 * @param content The change's data
	 * @param targetID The object targeted by the change
	 * @param votes Number of votes the change has gained
	 */
	public Change(String changedContent, String originalContent, String targetID, int votes){
		this.changedContent = changedContent;
		this.originalContent = originalContent;
		this.targetID = targetID;
		this.votes = votes;
	}
	
	/**
	 * This constructor should only be used for recreating a stored object.
	 * @param content The change's data
	 * @param targetID The object targeted by the change
	 * @param votes Number of votes the change has gained
	 * @param id The change's unique ID
	 */
	public Change(String changedContent, String originalContent, String targetID, int votes, String id){
		this.changedContent = changedContent;
		this.originalContent = originalContent;
		this.targetID = targetID;
		this.votes = votes;
		this.id = id;
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
	 * @return The change's number of votes
	 */
	public int getVotes() {
		return votes;
	}

	/**
	 * @param votes The change's new number of votes
	 */
	public void setVotes(int votes) {
		this.votes = votes;
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
}
