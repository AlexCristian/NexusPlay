package com.nexusplay.db;

import com.nexusplay.containers.Media;

import java.sql.*;
import java.util.ArrayList;

/**
 * Class that handles all database operations involving the Media container.
 * @author alex
 *
 */
public class MediaDatabase
{
	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS MediaDB(name VARCHAR(50), "
			+ "filename VARCHAR(100), poster VARCHAR(100), id VARCHAR(1000), year VARCHAR(300), "
			+ "views INT(1), category VARCHAR(50), season INT(1), episode INT(1), "
			+ "collectionid VARCHAR(1000), published INT(1), date TIMESTAMP DEFAULT CURRENT_TIMESTAMP);";

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
	 * Stores a Media item into the database.
	 * @param item Item to be stored in the table
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws Exception Thrown if a duplicate item of the pushed one already exists
	 */
    public static void pushMedia(Media item)
        throws SQLException, Exception
    {
        Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM MediaDB WHERE filename=?;";
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
            req = "SELECT * FROM MediaDB WHERE id=?;";
            stmt = con.prepareStatement(req);
            item.generateId();
            stmt.setString(1, item.getId());
            rs = stmt.executeQuery();
            if(rs.next())
                pp = true;
        } while(pp);
        req = "INSERT INTO MediaDB VALUES (? , ? , ? , ? , ? , 1, ? , ? , ? , ? , 0, DEFAULT);";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getName());
        stmt.setString(2, item.getFilename());
        stmt.setString(3, item.getPoster());
        stmt.setString(4, item.getId());
        stmt.setString(5, item.getYear());
        stmt.setString(6, item.getCategory());
        stmt.setInt(7, item.getSeason());
        stmt.setInt(8, item.getEpisode());
        stmt.setString(9, item.getCollectionID());
        stmt.executeUpdate();
    }

    /**
	 * Deletes an item from the table.
	 * @param item Item to delete
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 */
    public static void deleteMedia(Media item)
        throws SQLException
    {
        Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "DELETE FROM MediaDB WHERE id=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, item.getId());
        stmt.executeUpdate();
    }

    /**
     * Lookup media in the database by ID.
     * @param id The item's unique ID
     * @return A Media container with the specified object, null if it doesn't exist
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Media getMediaById(String id)
        throws SQLException
    {
        PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT * FROM MediaDB WHERE id = ?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, id);
        ResultSet rs = stmt.executeQuery();
        if(!rs.next())
        {
            return null;
        } else
        {
            Media item = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), 
            		rs.getString("category"), rs.getInt("published"));
            item.setPoster(rs.getString("poster"));
            item.setCollectionID(rs.getString("collectionid"));
            item.setEpisode(rs.getInt("episode"));
            item.setSeason(rs.getInt("season"));
            item.setYear(rs.getString("year"));
            item.setViews(rs.getInt("views"));
            return item;
        }
    }

    /**
     * Checks whether a file has been already added to the database or not.
     * @param filename Path to the file
     * @return True or false if the file doesn't exist in the table
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static boolean checkExists(String filename)
        throws SQLException
    {
        Connection con = getConnection();
        PreparedStatement stmt = null;
        String req = "SELECT * FROM MediaDB WHERE filename=?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, filename);
        ResultSet rs = stmt.executeQuery();
        return rs.next();
    }

    /**
     * Overwrites an existing Media object with the data provided.
     * @param item The item to be updated
     * @return False if there is no object to overwrite, true on success
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static boolean replaceMedia(Media item)
        throws SQLException
    {
        PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT * FROM MediaDB WHERE id like ?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, "%" + item.getId() + "%");
        ResultSet rs = stmt.executeQuery();
        if(!rs.next())
        {
            return false;
        } else
        {
        	req = "UPDATE MediaDB SET name=? ,filename=?, poster=?, category=? ,year=?, "
        			+ "collectionid=?, season=?, episode=?, published=? WHERE id=?;";
            stmt = con.prepareStatement(req);
            stmt.setString(1, item.getName());
            stmt.setString(2, item.getFilename());
            stmt.setString(3, item.getPoster());
            stmt.setString(4, item.getCategory());
            stmt.setString(5, item.getYear());
            stmt.setString(6, item.getCollectionID());
            stmt.setInt(7, item.getSeason());
            stmt.setInt(8, item.getEpisode());
            stmt.setInt(9, item.getPublished());
            stmt.setString(10, item.getId());
            
            stmt.executeUpdate();
            return true;
        }
    }
    
    /**
     * Increments the number of views of a video
     * @param mediaID The Media element's unique ID
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static void incrementViews(String mediaID) throws SQLException{
    	PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "UPDATE MediaDB SET views = views + 1 WHERE id = ?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, mediaID);
        stmt.executeUpdate();
    }
    
    /**
     * Updates notifications for the users subscribed to a certain collection.
     * @param media The item to propagate
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static void propagateMediaNotification(Media media)
            throws SQLException
        {
    		//only propagate if the object pertains to a collection
    		if(media.getCollectionID().equals(""))
    			return;
    			
            PreparedStatement stmt = null;
            Connection con = getConnection();           
            String req = "SELECT * FROM UsersDB WHERE subscriptions like ?;";
            stmt = con.prepareStatement(req);
            stmt.setString(1, "%" + media.getCollectionID() + "%");
            ResultSet rs = stmt.executeQuery();
            while(rs.next()){
            	String id = rs.getString("id");
            	req="UPDATE UsersDB SET notifications=concat(notifications, ?) WHERE id=?;";
            	stmt = con.prepareStatement(req);
            	stmt.setString(1, media.getId() + ";");
            	stmt.setString(2, id);
            	stmt.executeUpdate();
            }
        }

    /**
     * Fetch a list of the top popular media in the database.
     * @param size Maximum number of results to be returned
     * @return The top popular media, sorted by number of views
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Media[] getPopularMedia(int size)
        throws SQLException
    {
    	PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT category FROM MediaDB GROUP BY category;";
        stmt = con.prepareStatement(req);
        ResultSet rs = stmt.executeQuery();
        ArrayList<String> categs = new ArrayList<String>();
        while(rs.next()){
        	categs.add(rs.getString("category"));
        }
        
        ArrayList<Media> raw = new ArrayList<Media>();
        for(String currentCateg : categs){
	        req = "SELECT * FROM MediaDB WHERE category=? AND published = 1 ORDER BY views DESC LIMIT ?;";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, currentCateg);
	        stmt.setInt(2, size);
	        rs = stmt.executeQuery();
	        
	        while(rs.next())
	        {
	            Media temp = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
	            temp.setPoster(rs.getString("poster"));
	            temp.setCollectionID(rs.getString("collectionid"));
	            temp.setEpisode(rs.getInt("episode"));
	            temp.setSeason(rs.getInt("season"));
	            temp.setViews(rs.getInt("views"));
	            temp.setYear(rs.getString("year"));
	            raw.add(temp);
	        }
	        
        }
        
        Media result[] = new Media[raw.size()];
        for(int i = 0; i < raw.size(); i++)
            result[i] = (Media)raw.get(i);

        return result;
    }
    
    /**
     * Fetch a list of the unpublished media in the database.
     * @return A Media array containing the items lacking administrator approval
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Media[] getUnpublishedMedia() throws SQLException
        {
            PreparedStatement stmt = null;
            Connection con = getConnection();
            String req = "SELECT * FROM MediaDB WHERE published = 0;";
            stmt = con.prepareStatement(req);
            ResultSet rs = stmt.executeQuery();
            ArrayList<Media> raw = new ArrayList<Media>();
            while(rs.next())
            {
                Media temp = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
                temp.setPoster(rs.getString("poster"));
                temp.setCollectionID(rs.getString("collectionid"));
                temp.setEpisode(rs.getInt("episode"));
                temp.setSeason(rs.getInt("season"));
                temp.setViews(rs.getInt("views"));
                temp.setYear(rs.getString("year"));
                raw.add(temp);
            }

            Media result[] = new Media[raw.size()];
            for(int i = 0; i < raw.size(); i++)
                result[i] = (Media)raw.get(i);

            return result;
        }

    /**
     * Fetch a list of recent media in the database.
     * @param size Maximum number of results to be returned
     * @return The top recent media, sorted by the date they were added on
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Media[] getRecentMedia(int size)
        throws SQLException
    {
        PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT category FROM MediaDB GROUP BY category;";
        stmt = con.prepareStatement(req);
        ResultSet rs = stmt.executeQuery();
        ArrayList<String> categs = new ArrayList<String>();
        while(rs.next()){
        	categs.add(rs.getString("category"));
        }
        
        ArrayList<Media> raw = new ArrayList<Media>();
        for(String currentCateg : categs){
	        req = "SELECT * FROM (SELECT * FROM MediaDB WHERE category=? AND published = 1 AND collectionid <> '' GROUP BY collectionid UNION ALL SELECT * FROM MediaDB WHERE category = ? AND published = 1 AND collectionid = '') dum ORDER BY date DESC LIMIT ?;";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, currentCateg);
	        stmt.setString(2, currentCateg);
	        //ORDER BY date DESC LIMIT ?
	        stmt.setInt(3, size);
	        rs = stmt.executeQuery();
	        
	        while(rs.next())
	        {
	            Media temp = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
	            temp.setPoster(rs.getString("poster"));
	            temp.setCollectionID(rs.getString("collectionid"));
	            temp.setEpisode(rs.getInt("episode"));
	            temp.setSeason(rs.getInt("season"));
	            temp.setViews(rs.getInt("views"));
	            temp.setYear(rs.getString("year"));
	            raw.add(temp);
	        }
	        
        }
        
        Media result[] = new Media[raw.size()];
        for(int i = 0; i < raw.size(); i++)
            result[i] = (Media)raw.get(i);

        return result;
    }
    
    /**
     * Will attempt to extract a collection's category, assuming at least one episode has been published,
     * returns null on failure.
     * @param collectionID The collection's unique ID
     * @return The collection's category, null on empty return
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static String getCollectionCategoryById(String collectionID) throws SQLException{
    	PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT category FROM MediaDB WHERE collectionid = ? AND published = 1 LIMIT 1;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, collectionID);
        ResultSet rs = stmt.executeQuery();
        if(rs.next()){
        	return rs.getString("category");
        }
        return null;
    }

    /**
     * Fetch search results for a particular name query.
     * @param request The name of the Media object we're searching for
     * @param resultCap Maximum number of results to be returned
     * @return The search results, consisting in an array of Media objects
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static Media[] searchMedia(String request, int resultCap)
        throws SQLException
    {
        ArrayList<Media> raw = new ArrayList<Media>();
        PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT * FROM MediaDB WHERE published=1 AND collectionid!='' AND name like ? "
        		+ "GROUP BY collectionid UNION SELECT * FROM MediaDB WHERE published=1 AND "
        		+ "collectionid='' AND name like ?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, "%" + request + "%");
        stmt.setString(2, "%" + request + "%");
        Media item=null; int k=0;
        String collectionIDs = "";
        for(ResultSet rs = stmt.executeQuery(); rs.next() && k<=resultCap; raw.add(item), k++)
        {
            item = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
            item.setPoster(rs.getString("poster"));
            item.setYear(rs.getString("year"));
            item.setCollectionID(rs.getString("collectionid"));
            if(item.getCollectionID() != "")
            	collectionIDs += item.getCollectionID();
            item.setEpisode(rs.getInt("episode"));
            item.setSeason(rs.getInt("season"));
            item.setViews(rs.getInt("views"));
        }
        
        req = "SELECT * FROM CollectionsDB WHERE name like ?;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, "%" + request + "%");
        for(ResultSet rs = stmt.executeQuery(); rs.next() && k<=resultCap; )
        {
        	if(collectionIDs.contains(rs.getString("id")))
        		continue;
        	item = new Media("", "", "", getCollectionCategoryById(rs.getString("id")), 1);
        	item.setCollectionID(rs.getString("id"));
        	item.setPoster(rs.getString("poster"));
        	k++; raw.add(item);
        }

        Media result[] = new Media[raw.size()];
        for(int i = 0; i < raw.size(); i++)
            result[i] = (Media)raw.get(i);

        return result;
    }
    
    /**
     * Fetch the episodes pertaining to a collection.
     * @param collectionID The collection's unique ID
     * @return A two-dimensional array of type Media[season][episode]
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static ArrayList<ArrayList<Media>> getCollectionEpisodes(String collectionID) throws SQLException{
    	ArrayList<ArrayList<Media>> episodes = new ArrayList<ArrayList<Media>>();
    	PreparedStatement stmt = null;
        Connection con = getConnection();
        String req = "SELECT * FROM MediaDB WHERE published=1 AND collectionid=? ORDER BY season ASC, episode ASC;";
        stmt = con.prepareStatement(req);
        stmt.setString(1, collectionID);
        ResultSet rs = stmt.executeQuery();
        while(rs.next())
        {
            Media temp = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
            temp.setPoster(rs.getString("poster"));
            temp.setCollectionID(rs.getString("collectionid"));
            temp.setEpisode(rs.getInt("episode"));
            temp.setSeason(rs.getInt("season"));
            temp.setViews(rs.getInt("views"));
            temp.setYear(rs.getString("year"));
            while(episodes.size()<=temp.getSeason()){
            	episodes.add(new ArrayList<Media>());
            }
            episodes.get(temp.getSeason()).add(temp);
        }
        return episodes;
    }

}
