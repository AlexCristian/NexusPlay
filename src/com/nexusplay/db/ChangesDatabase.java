package com.nexusplay.db;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;

import com.nexusplay.containers.Change;
import com.nexusplay.containers.Subtitle;

/**
 * Class that handles all database operations involving the Change container.
 * @author alex
 *
 */
public class ChangesDatabase {

	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS ChangesDB(targetID VARCHAR(1000), "
			+ "changedContent VARCHAR(100), originalContent VARCHAR(100), id VARCHAR(1000), votes INT(1));";
	
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
	 * Stores a Change item into the database.
	 * @param item Item to be stored in the table
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws Exception Thrown if a duplicate item of the pushed one already exists
	 * @see Change
	 */
    public static void pushChange(Change item)
        throws     SQLException, Exception
    {
        Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM ChangesDB WHERE changedContent=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getChangedContent());
        ResultSet rs = stmt.executeQuery();
        if(rs.next())
            throw new Exception("Duplicate item!");
        boolean pp = true;
        
        //make sure the ID is unique
        do
        {
            pp = false;
            req = "SELECT * FROM ChangesDB WHERE id=?;";
            stmt = con.prepareStatement(req);
            item.generateId();
            stmt.setString(1, item.getId());
            rs = stmt.executeQuery();
            if(rs.next())
                pp = true;
        } while(pp);
        req = "INSERT INTO ChangesDB VALUES (? , ? , ? , ?, ?);";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getTargetID());
        stmt.setString(2, item.getChangedContent());
        stmt.setString(3, item.getOriginalContent());
        stmt.setString(4, item.getId());
        stmt.setInt(5, item.getVotes());
        stmt.executeUpdate();
    }
    
    /**
     * Fetches a change's target subtitle.
     * @param changeID The Change object's ID
     * @return The Subtitle object
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Subtitle getTargetSubtitle(String changeID) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM ChangesDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, changeID);
        ResultSet rs = stmt.executeQuery();
	    rs.next();
    	return SubtitlesDatabase.getSubtitleByID(rs.getString("targetID"));
    }
    
    /**
     * Fetches all changes associated with a particular subtitle.
     * @param targetID The Subtitle object's ID
     * @return A Change array with the query results
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Change[] getAssociatedSubtitles(String targetID) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM ChangesDB WHERE targetID=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, targetID);
        ResultSet rs = stmt.executeQuery();
        ArrayList<Change> raw = new ArrayList<Change>();
        while(rs.next()){
        	Change current = new Change(rs.getString("changedContent"), rs.getString("originalContent"), rs.getString("targetID"), rs.getInt("votes"), rs.getString("id"));
        	raw.add(current);
        }
        Change[] result = new Change[raw.size()];
        for(int i=0; i<raw.size(); i++){
        	result[i]=raw.get(i);
        }
        return result;
    }
    
    /**
     * Fetches a change by ID.
     * @param id The object's ID
     * @return The Change object
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Change getChangeByID(String id) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM ChangesDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        ResultSet rs = stmt.executeQuery();
        rs.next();
        return new Change(rs.getString("changedContent"), rs.getString("originalContent"), rs.getString("targetID"), rs.getInt("votes"), rs.getString("id"));
    }
    
    /**
     * Deletes a change from the database
     * @param id The object's ID
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static void deleteChange(String id) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "DELETE FROM ChangesDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        stmt.executeUpdate();
    }
	
	
}
