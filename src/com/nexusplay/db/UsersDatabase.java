package com.nexusplay.db;

import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.PreparedStatement;
import java.sql.Statement;

import com.nexusplay.containers.User;

/**
 * Class that handles all database operations involving the User container.
 * @author alex
 *
 */
public class UsersDatabase {

	private final static String tableDefinition = "CREATE TABLE IF NOT EXISTS UsersDB(email VARCHAR(100), "
			+ "nickname VARCHAR(100), password VARCHAR(100), watched TEXT, subscriptions TEXT, "
			+ "paused TEXT, notifications TEXT, id VARCHAR(50));";
	
	/**
	 * Opens a connection while at the same time making sure the table exists.
	 * @return Open database connection
	 * @throws SQLException In case we've got a connection error or the tableDefinition is erroneous
	 */
	private static Connection getConnection() throws SQLException{
		Statement stmt = null;
        Connection con = ConnectionManager.getConnection();
        stmt = con.createStatement();
        stmt.executeUpdate(tableDefinition);
        return con;
    }
	
	/**
	 * Stores a User item into the database.
	 * @param item Item to be stored in the table
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws Exception Thrown if a duplicate item of the pushed one already exists
	 */
	public static void pushUser(User item) throws Exception{
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE email=? OR nickname=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getEmail());
        stmt.setString(2, item.getNickname());
        ResultSet rs = stmt.executeQuery();
        if(rs.next())
            throw new Exception("User already registered!");
        req = "INSERT INTO UsersDB VALUES (?, ?, ?, ?, ?, ?, ?, ?);";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getEmail());
        stmt.setString(2, item.getNickname());
        stmt.setString(3, item.getPassword());
        stmt.setString(4, item.getWatchedSerialized());
        stmt.setString(5, item.getSubscriptionsSerialized());
        stmt.setString(6, item.getPausedSerialized());
        stmt.setString(7, item.getNotificationsSerialized());
        stmt.setString(8, item.getId());
        stmt.executeUpdate();
	}
	
	/**
	 * Checks if the email pertains to an account registered in the database.
	 * @param email Email address to be checked
	 * @return True or false if the email isn't registered
	 * @throws SQLException Thrown if a duplicate item of the pushed one already exists
	 */
	public static boolean isEmailValid(String email) throws SQLException{
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE email=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, email);
        ResultSet rs = stmt.executeQuery();
        if(rs.next())
            return false;
        return true;
	}
	
	/**
	 * Lookup a user in the database by it's email and password.
	 * @param email The user's email address
	 * @param password The password hash
	 * @return A User object populated with data from the database
	 * @throws Exception Thrown if the database is not accessible to us for whatever reason
	 */
	public static User getUser(String email, String password) throws Exception{
		if(isEmailValid(email))
			throw new Exception("Unregistered email!");
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE email=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, email);
        ResultSet rs = stmt.executeQuery();
        rs.next();
        if(!rs.getString("password").equals(password))
        	throw new Exception("Passwords don't match!");
        User user = new User(rs.getString("email"), rs.getString("watched"), rs.getString("nickname"), rs.getString("subscriptions"), rs.getString("password"), rs.getString("paused"), rs.getString("id"), rs.getString("notifications"));
        return user;
	}
	
	/**
	 * Lookup a user in the database by ID.
	 * @param id The user's unique ID
	 * @return A User object populated with the data stored in the database
	 * @throws Exception Thrown if the database is not accessible to us for whatever reason
	 */
	public static User getUserById(String id) throws Exception{
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        ResultSet rs = stmt.executeQuery();
        if(rs.next())
        	return new User(rs.getString("email"), rs.getString("watched"), rs.getString("nickname"), rs.getString("subscriptions"), rs.getString("password"), rs.getString("paused"), rs.getString("id"), rs.getString("notifications"));
        return null;
	}
	
	/**
	 * Deletes a user from the table.
	 * @param item Item to delete
	 * @throws Exception Thrown if the database is not accessible to us for whatever reason
	 */
	public static void deleteUser(User item) throws Exception{
		if(isEmailValid(item.getEmail()))
			throw new Exception("Unregistered email!");
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE email=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getEmail());
        ResultSet rs = stmt.executeQuery();
        rs.next();
        if(!rs.getString("password").equals(item.getPassword()))
        	throw new Exception("Passwords don't match!");
        req = "DELETE FROM UsersDB WHERE email=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getEmail());
        stmt.executeUpdate();
	}
	
	/**
	 * Overwrites an existing User object with the data provided.
	 * @param item The item to be updated
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 */
	public static void replaceUser(User item) throws SQLException{
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "UPDATE UsersDB SET email=?, nickname=?, password=?, watched=?, subscriptions=?, paused=?, notifications=? WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getEmail());
        stmt.setString(2, item.getNickname());
        stmt.setString(3, item.getPassword());
        stmt.setString(4, item.getWatchedSerialized());
        stmt.setString(5, item.getSubscriptionsSerialized());
        stmt.setString(6, item.getPausedSerialized());
        stmt.setString(7, item.getNotificationsSerialized());
        stmt.setString(8, item.getId());
        stmt.executeUpdate();
	}
	
	/**
	 * Flags a Media object as watched.
	 * @param userID The user's unique ID
	 * @param mediaID The video's unique ID
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 */
	public static void setMediaWatched(String userID, String mediaID) throws SQLException{
		Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM UsersDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, userID);
        ResultSet rs = stmt.executeQuery();
        if(!rs.next())
        	return;
        if(!rs.getString("watched").contains(mediaID+";")){
        	req="UPDATE UsersDB SET watched=concat(watched, ?) WHERE id=?;";
        	stmt = con.prepareStatement(req);
        	stmt.setString(1, mediaID + ";");
        	stmt.setString(2, userID);
        	stmt.executeUpdate();
        }
        if(rs.getString("notifications").contains(mediaID+";")){
        	req="UPDATE UsersDB SET notifications=? WHERE id=?;";
        	stmt = con.prepareStatement(req);
        	stmt.setString(1, rs.getString("notifications").replace(mediaID+";", ""));
        	stmt.setString(2, userID);
        	stmt.executeUpdate();
        }
	}

}
