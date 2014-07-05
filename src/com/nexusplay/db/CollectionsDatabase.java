package com.nexusplay.db;

import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.PreparedStatement;
import java.util.ArrayList;
import com.nexusplay.containers.Collection;

/**
 * Class that handles all database operations involving the Collection container.
 * @author alex
 *
 */
public class CollectionsDatabase {

	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS "
			+ "CollectionsDB(name VARCHAR(50), poster VARCHAR(100), "
			+ "id VARCHAR(1000), year VARCHAR(300));";
	
	/**
	 * Opens a connection while at the same time making sure the table exists.
	 * @return Open database connection
	 * @throws SQLException In case we've got a connection error or the tableDefinition is erroneous
	 */
	private static Connection getConnection() throws SQLException{
    	PreparedStatement stmt = null;
        Connection con = ConnectionManager.getConnection();
        stmt = con.prepareStatement(tableDefinition);
        stmt.executeUpdate();
        return con;
    }
	
	/**
	 * Stores a Collection item into the database.
	 * @param item Item to be stored in the table
	 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	 * @throws Exception Thrown if a duplicate item of the pushed one already exists
	 */
	public static void pushCollection(Collection item)
	        throws SQLException, Exception
	    {
	        Connection con = getConnection();
	        PreparedStatement stmt = null;
	        String req = "SELECT * FROM CollectionsDB WHERE name=?;";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, item.getName());
	        ResultSet rs = stmt.executeQuery();
	        if(rs.next())
	            throw new Exception("Duplicate item!");
	        boolean pp = true;
	        
	        //make sure the ID is unique
	        do
	        {
	            pp = false;
	            item.generateId();
	            req = "SELECT * FROM CollectionsDB WHERE id=?;";
	            stmt = con.prepareStatement(req);
	            stmt.setString(1, item.getID());
	            rs = stmt.executeQuery();
	            if(rs.next())
	                pp = true;
	        } while(pp);
	        req = "INSERT INTO CollectionsDB VALUES (? , ? , ? , ?);";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, item.getName());
	        stmt.setString(2, item.getPoster());
	        stmt.setString(3, item.getID());
	        stmt.setString(4, item.getYear());
	        stmt.executeUpdate();
	    }

		/**
		 * Deletes an item from the table.
		 * @param item Item to delete
		 * @throws SQLException Thrown if the database is not accessible to us for whatever reason
		 */
	    public static void deleteCollection(Collection item)
	        throws SQLException
	    {
	        Connection con = getConnection();
	        PreparedStatement stmt = null;
	        String req = "DELETE FROM CollectionsDB WHERE id=?;";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, item.getID());
	        stmt.executeUpdate();
	    }
	    
	    /**
	     * Lookup a collection in the database by its ID.
	     * @param id The collection's unique ID
	     * @return A Collection container with the specified object, null if it doesn't exist
	     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	     */
	    public static Collection getCollectionById(String id)
	        throws SQLException
	    {
	        PreparedStatement stmt = null;
	        Connection con = getConnection();
	        String req = "SELECT * FROM CollectionsDB WHERE id = ?;";
	        stmt = con.prepareStatement(req);
	        stmt.setString(1, id);
	        ResultSet rs = stmt.executeQuery();
	        if(!rs.next())
	        {
	            return null;
	        } else
	        {
	        	Collection item = new Collection(rs.getString("name"), rs.getString("id"));
	            item.setPoster(rs.getString("poster"));
	            item.setYear(rs.getString("year"));
	            return item;
	        }
	    }
	    
	    /**
	     * Fetches the name associated with a particular ID.
	     * @param id The item's unique ID
	     * @return The item's name
	     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	     */
	    public static String matchIdWithName(String id)
		        throws SQLException
		    {
		        PreparedStatement stmt = null;
		        Connection con = getConnection();
		        String req = "SELECT * FROM CollectionsDB WHERE id = ?;";
		        stmt = con.prepareStatement(req);
		        stmt.setString(1, id);
		        ResultSet rs = stmt.executeQuery();
		        if(!rs.next())
		        {
		            return null;
		        } else
		        {
		        	return rs.getString("name");
		        }
		    }
	    
	    /**
	     * Lookup a collection in the database by its name.
	     * @param name The item's name
	     * @return A Collection container with the specified object, null if it doesn't exist
	     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	     */
	    public static Collection getCollectionByName(String name)
		        throws SQLException
		    {
		        PreparedStatement stmt = null;
		        Connection con = getConnection();
		        String req = "SELECT * FROM CollectionsDB WHERE name = ?;";
		        stmt = con.prepareStatement(req);
		        stmt.setString(1, name);
		        ResultSet rs = stmt.executeQuery();
		        if(!rs.next())
		        {
		            return null;
		        } else
		        {
		        	Collection item = new Collection(rs.getString("name"), rs.getString("id"));
		            item.setPoster(rs.getString("poster"));
		            item.setYear(rs.getString("year"));
		            return item;
		        }
		    }
	    
	    /**
	     * Fetch search results for a particular name query.
	     * @param request The name of the collection we're searching for
	     * @param resultCap Maximum number of results to be returned
	     * @return The search results, consisting in an array of Collection objects
	     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	     */
	    public static Collection[] searchCollections(String request, int resultCap)
	            throws SQLException
	        {
	            ArrayList<Collection> raw = new ArrayList<Collection>();
	            PreparedStatement stmt = null;
	            Connection con = getConnection();
	            String req = "SELECT * FROM CollectionsDB WHERE name like '%?%';";
	            stmt = con.prepareStatement(req);
	            stmt.setString(1, request);
	            Collection item; int k=0;
	            for(ResultSet rs = stmt.executeQuery(); rs.next() && k<=resultCap; raw.add(item), k++)
	            {
	                item = new Collection(rs.getString("name"), rs.getString("id"));
	            }

	            Collection result[] = new Collection[raw.size()];
	            for(int i = 0; i < raw.size(); i++)
	                result[i] = raw.get(i);

	            return result;
	        }
	    
	    /**
	     * Overwrites an existing collection with the data provided.
	     * @param item The item to be updated
	     * @return False if there is no object to overwrite, true on success
	     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
	     */
	    public static boolean replaceCollection(Collection item)
	            throws SQLException
	        {
	            PreparedStatement stmt = null;
	            Connection con = getConnection();
	            String req = "SELECT * FROM CollectionsDB WHERE id like '%?%';";
	            stmt = con.prepareStatement(req);
	            stmt.setString(1, item.getID());
	            ResultSet rs = stmt.executeQuery();
	            if(!rs.next())
	            {
	                return false;
	            } else
	            {
	                req = "UPDATE CollectionsDB SET name=?, poster=?, year=? WHERE id=?;";
	                stmt = con.prepareStatement(req);
	                stmt.setString(1, item.getName());
	                stmt.setString(2, item.getPoster());
	                stmt.setString(3, item.getYear());
	                stmt.setString(4, item.getID());
	                stmt.executeUpdate();
	                return true;
	            }
	        }
}
