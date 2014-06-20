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
			//CHECK HERE FOR BUGS
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
	
	
	public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }
	
	public String getEmail() {
		return email;
	}
	public void setEmail(String email) {
		this.email = email;
	}
	public String getNickname() {
		return nickname;
	}
	public void setNickname(String nickname) {
		this.nickname = nickname;
	}
	public String getPassword() {
		return password;
	}
	public void setPassword(String password) {
		this.password = password;
	}
	public String getWatchedSerialized(){
		String resp = new String();
		for(String item : watched){
			resp+=item + ";";
		}
		return resp;
	}
	public ArrayList<String> getWatched() {
		return watched;
	}
	public boolean isWatched(String mediaID){
		return watched.contains(mediaID);
	}
	public void setWatched(ArrayList<String> watched) {
		this.watched = watched;
	}
	public String getSubscriptionsSerialized(){
		String resp = new String();
		for(String item : subscriptions){
			resp+=item + ";";
		}
		return resp;
	}
	public ArrayList<String> getSubscriptions() {
		return subscriptions;
	}
	public void setSubscriptions(ArrayList<String> subscriptions) {
		this.subscriptions = subscriptions;
	}
	public void subscribe(String collectionID){
		subscriptions.add(collectionID);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}
	public void unsubscribe(String collectionID){
		subscriptions.remove(collectionID);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}
	public String getPausedSerialized(){
		String resp = new String();
		
		for (Map.Entry<String, String> entry : paused.entrySet()) {
			resp+= entry.getKey() + "," +  entry.getValue() + ";";
		}
		
		return resp;
	}
	public void storePausedState(String id, String time){
		paused.put(id, time);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}
	public void popPausedState(String id){
		paused.remove(id);
		try {
			UsersDatabase.replaceUser(this);
		} catch (SQLException e) {
			// TODO Auto-generated catch block
			e.printStackTrace();
		}
	}
	public String getPausedTimeByID(String id){
		if(paused.containsKey(id))
			return paused.get(id);
		else return "0";
	}
	public HashMap<String, String> getPaused() {
		return paused;
	}
	public void setPaused(HashMap<String, String> paused) {
		this.paused = paused;
	}
	public String getId() {
		return id;
	}
	public void setId(String id) {
		this.id = id;
	}
	public ArrayList<String> getNotifications() {
		return notifications;
	}
	public String getNotificationsSerialized(){
		String resp = new String();
		for(String item : notifications){
			resp+=item + ";";
		}
		return resp;
	}
	public void setNotifications(ArrayList<String> notifications) {
		this.notifications = notifications;
	}
	
}
