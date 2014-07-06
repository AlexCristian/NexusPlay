package com.nexusplay.containers;

import java.math.BigInteger;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.Map;
import java.util.StringTokenizer;

import com.nexusplay.db.UsersDatabase;
import com.nexusplay.security.Crypto;
import com.nexusplay.security.RandomContainer;

/**
 * This class represents the data that makes up a user.
 * @author alex
 *
 */
public class User {

	private String email, nickname, password, id;
	private ArrayList<String> watched, subscriptions, notifications;
	private HashMap<String, String> paused;
	
	/**
	 * This constructor should only be used by the database manager.
	 * It assumes that the password has already been double-encrypted.
	 * <p>
	 * @param PMemail user's email
	 * @param PMwatched	raw database dump of watched media
	 * @param PMnickname user's nickname
	 * @param PMsubscriptions raw database dump of subscriptions
	 * @param PMpassword user's password (2x SHA-1)
	 * @param PMpaused raw database dump of paused media
	 */
	public User(String PMemail, String PMwatched, String PMnickname, String PMsubscriptions, String PMpassword, String PMpaused, String PMID, String PMnotifications){
		email = PMemail; nickname = PMnickname; id=PMID;
		watched=new ArrayList<String>();
		StringTokenizer watchedTokenizer = new StringTokenizer(PMwatched, ";");
		while(watchedTokenizer.hasMoreElements()){
			watched.add(watchedTokenizer.nextToken());
		}
		subscriptions=new ArrayList<String>();
		StringTokenizer subscriptionsTokenizer = new StringTokenizer(PMsubscriptions, ";");
		while(subscriptionsTokenizer.hasMoreElements()){
			subscriptions.add(subscriptionsTokenizer.nextToken());
		}
		notifications=new ArrayList<String>();
		StringTokenizer notificationsTokenizer = new StringTokenizer(PMnotifications, ";");
		while(notificationsTokenizer.hasMoreElements()){
			notifications.add(notificationsTokenizer.nextToken());
		}
		password = PMpassword;
		paused = new HashMap<String, String>();
		StringTokenizer pausedTokenizer = new StringTokenizer(PMpaused, ";");
		while(pausedTokenizer.hasMoreElements()){
			String item = pausedTokenizer.nextToken();
			paused.put(item.substring(0, item.indexOf(",")), item.substring(item.indexOf(",")+1));
		}
	}
	/**
	 * This constructor receives basic user data and encrypts
	 * the password with the SHA-1 algorithm.
	 * @param PMemail user's email
	 * @param PMnickname user's nickname
	 * @param PMpassword user's raw password
	 */
	public User(String PMemail, String PMnickname, String PMpassword){
		email = PMemail; nickname = PMnickname; generateId();
		watched=new ArrayList<String>();
		subscriptions=new ArrayList<String>();
		notifications=new ArrayList<String>();
		password = Crypto.encryptPassword(PMpassword);
		paused = new HashMap<String, String>();
	}
	
	/**
	 * Generates a new unique ID
	 */
	public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }
	
	/**
	 * 
	 * @return The user's email address
	 */
	public String getEmail() {
		return email;
	}
	
	/**
	 * 
	 * @param email The user's new email address
	 */
	public void setEmail(String email) {
		this.email = email;
	}
	
	/**
	 * 
	 * @return The user's nickname
	 */
	public String getNickname() {
		return nickname;
	}
	
	/**
	 * 
	 * @param nickname The user's new nickname
	 */
	public void setNickname(String nickname) {
		this.nickname = nickname;
	}
	
	/**
	 * 
	 * @return The user's password hash
	 */
	public String getPassword() {
		return password;
	}
	
	/**
	 * 
	 * @param password The user's new password hash
	 */
	public void setPassword(String password) {
		this.password = password;
	}
	
	/**
	 * 
	 * @return The serialized IDs of the Media objects the user has watched
	 */
	public String getWatchedSerialized(){
		String resp = new String();
		for(String item : watched){
			resp+=item + ";";
		}
		return resp;
	}
	
	/**
	 * 
	 * @return An array containing the IDs of the Media objects the user has watched
	 */
	public ArrayList<String> getWatched() {
		return watched;
	}
	
	/**
	 * Searches for the parameter ID in the viewed Media IDs array.
	 * @param mediaID The ID to look for
	 * @return True or false if the ID has not been watched
	 */
	public boolean isWatched(String mediaID){
		return watched.contains(mediaID);
	}
	
	/**
	 * Adds a Media ID to the watched list.
	 * @param watched The watched object's ID
	 */
	public void setWatched(ArrayList<String> watched) {
		this.watched = watched;
	}
	
	/**
	 * 
	 * @return The serialized IDs of the Collections the user is subscribed to
	 */
	public String getSubscriptionsSerialized(){
		String resp = new String();
		for(String item : subscriptions){
			resp+=item + ";";
		}
		return resp;
	}
	
	/**
	 * 
	 * @return An array containing the IDs of the Collection objects the user 
	 * is subscribed to
	 */
	public ArrayList<String> getSubscriptions() {
		return subscriptions;
	}
	
	/**
	 * 
	 * @param subscriptions The new subscription list
	 */
	public void setSubscriptions(ArrayList<String> subscriptions) {
		this.subscriptions = subscriptions;
	}
	
	/**
	 * Subscribes a User to the specified Collection.
	 * @param collectionID The Collection's ID 
	 */
	public void subscribe(String collectionID){
		subscriptions.add(collectionID);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}
	
	/**
	 * Unsubscribes a User from the specified Collection.
	 * @param collectionID The Collection ID to be removed
	 */
	public void unsubscribe(String collectionID){
		subscriptions.remove(collectionID);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			e.printStackTrace();
		}
	}
	
	/**
	 * 
	 * @return The serialized IDs of the Media objects the user has paused
	 */
	public String getPausedSerialized(){
		String resp = new String();
		
		for (Map.Entry<String, String> entry : paused.entrySet()) {
			resp+= entry.getKey() + "," +  entry.getValue() + ";";
		}
		
		return resp;
	}
	
	/**
	 * Stores the paused state of a Media object.
	 * @param id The Media object's unique ID
	 * @param time The point in time when the video was paused
	 */
	public void storePausedState(String id, String time){
		paused.put(id, time);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			e.printStackTrace();
		}
	}
	
	/**
	 * Removes an object's paused state from the database
	 * @param id The Media object's unique ID
	 */
	public void popPausedState(String id){
		paused.remove(id);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			e.printStackTrace();
		}
	}
	
	/**
	 * Fetches the pause time of a stored item
	 * @param id The Media's ID
	 * @return The time at which the video was paused
	 */
	public String getPausedTimeByID(String id){
		if(paused.containsKey(id))
			return paused.get(id);
		else return "0";
	}
	
	/**
	 * 
	 * @return A HashMap with the user's paused Media
	 */
	public HashMap<String, String> getPaused() {
		return paused;
	}
	
	/**
	 * 
	 * @param paused The new HashMap to store the user's paused Media
	 */
	public void setPaused(HashMap<String, String> paused) {
		this.paused = paused;
	}
	
	/**
	 * 
	 * @return The user's unique ID
	 */
	public String getId() {
		return id;
	}
	
	/**
	 * 
	 * @param id The user's new unique ID
	 */
	public void setId(String id) {
		this.id = id;
	}
	
	/**
	 * The user's new episode notifications for the 
	 * Collections the User's subscribed to.
	 * @return An array containing Media IDs
	 */
	public ArrayList<String> getNotifications() {
		return notifications;
	}
	
	/**
	 * The user's serialized new episode notifications
	 * for the Collections the User's subscribed to.
	 * @return A serialized String containing Media IDs
	 */
	public String getNotificationsSerialized(){
		String resp = new String();
		for(String item : notifications){
			resp+=item + ";";
		}
		return resp;
	}
	
	/**
	 * Sets a new array for the user's new episode notifications.
	 * @param notifications The array that holds our episodes
	 */
	public void setNotifications(ArrayList<String> notifications) {
		this.notifications = notifications;
	}
	
}
