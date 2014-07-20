package com.nexusplay.db;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;

import com.nexusplay.containers.Subtitle;

/**
 * Class that handles all database operations involving the Subtitle container.
 * @author alex
 *
 */
public class SubtitlesDatabase {

	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS SubtitlesDB(mediaID VARCHAR(1000), "
			+ "language VARCHAR(100), id VARCHAR(1000), filename VARCHAR(100));";

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
	 * Stores a Subtitle item into the database.
	 * @param item Item to be stored in the table
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws Exception Thrown if a duplicate item of the pushed one already exists
	 * @see Subtitle
	 */
    public static void pushSubtitle(Subtitle item)
        throws SQLException, Exception
    {
        Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM SubtitlesDB WHERE filename=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getFilename());
        ResultSet rs = stmt.executeQuery();
        if(rs.next())
            throw new Exception("Duplicate item!");
        boolean pp = true;
        
        //make sure the ID is unique
        do
        {
            pp = false;
            req = "SELECT * FROM SubtitlesDB WHERE id=?;";
            stmt = con.prepareStatement(req);
            item.generateId();
            stmt.setString(1, item.getId());
            rs = stmt.executeQuery();
            if(rs.next())
                pp = true;
        } while(pp);
        req = "INSERT INTO SubtitlesDB VALUES (? , ? , ? , ?);";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getMediaID());
        stmt.setString(2, item.getLanguage());
        stmt.setString(3, item.getId());
        stmt.setString(4, item.getFilename());
        stmt.executeUpdate();
    }
    
    /**
     * Fetches all subtitles associated with a particular video.
     * @param mediaID The Media object's ID
     * @return A Subtitle array with the query results
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Subtitle[] getAssociatedSubtitles(String mediaID) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM SubtitlesDB WHERE mediaID=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, mediaID);
        ResultSet rs = stmt.executeQuery();
        ArrayList<Subtitle> raw = new ArrayList<Subtitle>();
        while(rs.next()){
        	Subtitle current = new Subtitle(rs.getString("mediaID"), rs.getString("language"), rs.getString("id"), rs.getString("filename"));
        	raw.add(current);
        }
        Subtitle[] result = new Subtitle[raw.size()];
        for(int i=0; i<raw.size(); i++){
        	result[i]=raw.get(i);
        }
        return result;
    }
    
    /**
     * Fetches a subtitle by ID.
     * @param id The object's ID
     * @return The Subtitle object
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Subtitle getSubtitleByID(String id) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM SubtitlesDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        ResultSet rs = stmt.executeQuery();
        rs.next();
        return new Subtitle(rs.getString("mediaID"), rs.getString("language"), rs.getString("id"), rs.getString("filename"));
    }
    
    /**
     * Deletes a subtitle from the database
     * @param id The object's ID
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static void deleteSubtitle(String id) throws SQLException{
    	Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "DELETE FROM SubtitlesDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        stmt.executeUpdate();
    }
}
