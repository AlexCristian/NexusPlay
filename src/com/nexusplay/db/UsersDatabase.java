package com.nexusplay.db;

import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;

import com.nexusplay.containers.User;

public class UsersDatabase {

	private final static String tableDefinition = "CREATE TABLE IF NOT EXISTS UsersDB(email VARCHAR(100), nickname VARCHAR(100), password VARCHAR(100), watched TEXT, subscriptions TEXT, paused TEXT, id VARCHAR(50));";
	private static Connection getConnection() throws SQLException{
    	Statement stmt = null;
        Connection con = ConnectionManager.getConnection();
        stmt = con.createStatement();
        stmt.executeUpdate(tableDefinition);
        return con;
    }
	
	public static void pushUser(User item) throws Exception{
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE email='" + item.getEmail() + "';";
        ResultSet rs = stmt.executeQuery(req);
        if(rs.next())
            throw new Exception("User already registered!");
        stmt = con.createStatement();
        req = "INSERT INTO UsersDB VALUES ('"+item.getEmail()+"', '"+item.getNickname()+"', '"+item.getPassword()+"', '"+item.getWatchedSerialized()+"', '"+item.getSubscriptionsSerialized()+"' ,'"+item.getPausedSerialized()+"', '" + item.getId() + "');";
        stmt.executeUpdate(req);
	}
	public static boolean isEmailValid(String email) throws SQLException{
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE email='" + email + "';";
        ResultSet rs = stmt.executeQuery(req);
        if(rs.next())
            return false;
        return true;
	}
	public static User getUser(String email, String password) throws Exception{
		if(isEmailValid(email))
			throw new Exception("Unregistered email!");
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE email='" + email + "';";
        ResultSet rs = stmt.executeQuery(req);
        rs.next();
        if(!rs.getString("password").equals(password))
        	throw new Exception("Passwords don't match!");
        User user = new User(rs.getString("email"), rs.getString("watched"), rs.getString("nickname"), rs.getString("subscriptions"), rs.getString("password"), rs.getString("paused"), rs.getString("id"));
        return user;
	}
	public static User getUserById(String id) throws Exception{
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE id='" + id + "';";
        ResultSet rs = stmt.executeQuery(req);
        if(rs.next())
        	return new User(rs.getString("email"), rs.getString("watched"), rs.getString("nickname"), rs.getString("subscriptions"), rs.getString("password"), rs.getString("paused"), rs.getString("id"));
        return null;
	}
	public static void deleteUser(User item) throws Exception{
		if(isEmailValid(item.getEmail()))
			throw new Exception("Unregistered email!");
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE email='" + item.getEmail() + "';";
        ResultSet rs = stmt.executeQuery(req);
        rs.next();
        if(!rs.getString("password").equals(item.getPassword()))
        	throw new Exception("Passwords don't match!");
        stmt = con.createStatement();
        req = "DELETE FROM UsersDB WHERE email='" + item.getEmail() + "';";
        stmt.executeUpdate(req);
	}
	public static void replaceUser(User item) throws SQLException{
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "UPDATE UsersDB SET email='"+item.getEmail()+"', nickname='"+item.getNickname()+"', password='"+item.getPassword()+"', watched='"+item.getWatchedSerialized()+"', subscriptions='"+item.getSubscriptionsSerialized()+"', paused='"+item.getPausedSerialized()+"' WHERE id='" + item.getId() + "';";
        stmt.executeUpdate(req);
	}
	public static void setMediaWatched(String userID, String mediaID) throws SQLException{
		Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = "SELECT * FROM UsersDB WHERE id='" + userID + "';";
        ResultSet rs = stmt.executeQuery(req);
        if(!rs.next())
        	return;
        if(!rs.getString("watched").contains(mediaID+";")){
        	req="UPDATE UsersDB SET watched=concat(watched,'" + mediaID + ";') WHERE id='" + userID + "';";
        	stmt = con.createStatement();
        	stmt.executeUpdate(req);
        }
	}
}
