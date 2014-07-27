package com.nexusplay.containers;

import java.math.BigInteger;
import java.sql.SQLException;
import java.util.ArrayList;

import com.nexusplay.db.MediaDatabase;
import com.nexusplay.security.RandomContainer;

/**
 * Stores collectible media (part of a series, a TV show).
 * @author alex
 *
 */
public class Collection {

	private String name, id, poster, year;
	private ArrayList<ArrayList<Media>> episodes;
	private int seasons;
	
	/**
	 * Creates a new Collection object and automatically populates it
	 * with the episodes associated to the collectionID provided.
	 * @param name The name of the collection
	 * @param collectionID Collection's unique ID
	 */
	public Collection(String name, String collectionID){
		this.name = name;
		this.id = collectionID;
		try {
			episodes = MediaDatabase.getCollectionEpisodes(collectionID);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
		seasons = episodes.size()-1;
	}
	
	/**
	 * Creates a blank collection with the parameters provided.
	 * @param name Name of the collection
	 * @param year Year of publication
	 * @param poster URI pointing towards the poster image
	 */
	public Collection(String name, String year, String poster){
		this.name = name;
		this.year = year;
		this.poster = poster;
		generateId();
	}
	/**
	 * Assigns a unique ID to the Collection.
	 */
	public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }

	/**
	 * 
	 * @param season Season number
	 * @param episode Episode number
	 * @return Media object representing the corresponding episode
	 */
	public Media getEpisode(int season, int episode){
		return episodes.get(season).get(episode);
	}
	/**
	 * 
	 * @return The name of the collection.
	 */
	public String getName() {
		return name;
	}

	/**
	 * 
	 * @param name The new name of the collection
	 */
	public void setName(String name) {
		this.name = name;
	}

	/**
	 * 
	 * @return The unique ID of the collection
	 */
	public String getID() {
		return id;
	}

	/**
	 * Manually sets the collection's unique ID; not
	 * recommended since the constructor should take
	 * care of assigning the proper ID at instantiation time.
	 * @param collectionID The new UID for the collection
	 */
	public void setID(String collectionID) {
		this.id = collectionID;
	}

	/**
	 * 
	 * @return URI pointing towards the poster image
	 */
	public String getPoster() {
		return poster;
	}

	/**
	 * 
	 * @param poster URI pointing towards the new poster image
	 */
	public void setPoster(String poster) {
		this.poster = poster;
	}

	/**
	 * 
	 * @return The collection's year of publication
	 */
	public String getYear() {
		return year;
	}

	/**
	 * 
	 * @param year The collection's new year of publication
	 */
	public void setYear(String year) {
		this.year = year;
	}

	/**
	 * 
	 * @return The number of seasons the collection holds
	 */
	public int getSeasons() {
		return seasons;
	}
	
	/**
	 * 
	 * @param seasons The new number of season within collection
	 */
	public void setSeasons(int seasons) {
		this.seasons = seasons;
	}

	/**
	 * Returns an ArrayList populated with the Media contained
	 * by the collection. Arranged by array[season][episode].
	 * @return The episodes held within the collection
	 */
	public ArrayList<ArrayList<Media>> getEpisodes() {
		return episodes;
	}

	/**
	 * Manually sets the episodes the collection is to contain.
	 * @param episodes The ArrayList containing the episodes
	 */
	public void setEpisodes(ArrayList<ArrayList<Media>> episodes) {
		this.episodes = episodes;
	}

}
