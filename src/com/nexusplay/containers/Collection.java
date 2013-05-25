package com.nexusplay.containers;

import java.math.BigInteger;
import java.sql.SQLException;
import java.util.ArrayList;

import com.nexusplay.db.MediaDatabase;
import com.nexusplay.security.RandomContainer;

public class Collection {

	private String name, id, poster, year;
	private ArrayList<ArrayList<Media>> episodes;
	
	public Collection(String name, String collectionID){
		this.name = name;
		this.id = collectionID;
		try {
			episodes = MediaDatabase.getCollectionEpisodes(collectionID);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
		
	}
	
	public Collection(String name, String year, String poster){
		this.name = name;
		this.year = year;
		this.poster = poster;
		
	}
	
	public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }

	public Media getEpisode(int season, int episode){
		return episodes.get(season).get(episode);
	}
	public String getName() {
		return name;
	}

	public void setName(String name) {
		this.name = name;
	}

	public String getID() {
		return id;
	}

	public void setID(String collectionID) {
		this.id = collectionID;
	}

	public String getPoster() {
		return poster;
	}

	public void setPoster(String poster) {
		this.poster = poster;
	}

	public String getYear() {
		return year;
	}

	public void setYear(String year) {
		this.year = year;
	}

	public ArrayList<ArrayList<Media>> getEpisodes() {
		return episodes;
	}

	public void setEpisodes(ArrayList<ArrayList<Media>> episodes) {
		this.episodes = episodes;
	}
}
