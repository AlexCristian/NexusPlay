package com.nexusplay.db;

import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;

import com.nexusplay.containers.Collection;
import com.nexusplay.containers.Media;


public class CollectionsDatabase {

	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS CollectionsDB(name VARCHAR(50), poster VARCHAR(100), id VARCHAR(1000), year VARCHAR(300));";
	private static Connection getConnection() throws SQLException{
    	Statement stmt = null;
        Connection con = ConnectionManager.getConnection();
        stmt = con.createStatement();
        stmt.executeUpdate(tableDefinition);
        return con;
    }
	
	public static void pushCollection(Collection item)
	        throws SQLException, Exception
	    {
	        Connection con = getConnection();
	        Statement stmt = null;
	        stmt = con.createStatement();
	        String req = (new StringBuilder("SELECT * FROM CollectionsDB WHERE name='")).append(item.getName()).append("';").toString();
	        ResultSet rs = stmt.executeQuery(req);
	        if(rs.next())
	            throw new Exception("Duplicate item!");
	        boolean pp = true;
	        do
	        {
	            pp = false;
	            stmt = con.createStatement();
	            item.generateId();
	            req = (new StringBuilder("SELECT * FROM CollectionsDB WHERE id='")).append(item.getID()).append("';").toString();
	            rs = stmt.executeQuery(req);
	            if(rs.next())
	                pp = true;
	        } while(pp);
	        stmt = con.createStatement();
	        req = "INSERT INTO CollectionsDB VALUES ('"+item.getName()+"' , '"+item.getPoster()+"' , '"+item.getID()+"' , '"+item.getYear() + "');";
	        stmt.executeUpdate(req);
	    }

	    public static void deleteCollection(Collection item)
	        throws SQLException
	    {
	        Connection con = getConnection();
	        Statement stmt = null;
	        stmt = con.createStatement();
	        String req = (new StringBuilder("DELETE FROM CollectionsDB WHERE id='")).append(item.getID()).append("';").toString();
	        stmt.executeUpdate(req);
	    }

	    public static Collection getCollectionById(String id)
	        throws SQLException
	    {
	        Statement stmt = null;
	        Connection con = getConnection();
	        stmt = con.createStatement();
	        String req = new String("SELECT * FROM CollectionsDB WHERE ");
	        req = (new StringBuilder(String.valueOf(req))).append("id = '").append(id).append("';").toString();
	        ResultSet rs = stmt.executeQuery(req);
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
	    
	    public static Collection getCollectionByName(String name)
		        throws SQLException
		    {
		        Statement stmt = null;
		        Connection con = getConnection();
		        stmt = con.createStatement();
		        String req = new String("SELECT * FROM CollectionsDB WHERE ");
		        req = (new StringBuilder(String.valueOf(req))).append("name = '").append(name).append("';").toString();
		        ResultSet rs = stmt.executeQuery(req);
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
	    
	    public static Collection[] searchCollections(String request, int resultCap)
	            throws SQLException
	        {
	            ArrayList<Collection> raw = new ArrayList<Collection>();
	            Statement stmt = null;
	            Connection con = getConnection();
	            stmt = con.createStatement();
	            String req = new String("SELECT * FROM CollectionsDB WHERE ");
	            req = (new StringBuilder(String.valueOf(req))).append("name like '%").append(request).append("%';").toString();
	            Collection item; int k=0;
	            for(ResultSet rs = stmt.executeQuery(req); rs.next() && k<=resultCap; raw.add(item), k++)
	            {
	                item = new Collection(rs.getString("name"), rs.getString("id"));
	            }

	            Collection result[] = new Collection[raw.size()];
	            for(int i = 0; i < raw.size(); i++)
	                result[i] = raw.get(i);

	            return result;
	        }
	    public static boolean replaceCollection(Collection item)
	            throws SQLException
	        {
	            Statement stmt = null;
	            Connection con = getConnection();
	            stmt = con.createStatement();
	            String req = new String("SELECT * FROM CollectionsDB WHERE ");
	            req = (new StringBuilder(String.valueOf(req))).append("id like '%").append(item.getID()).append("%';").toString();
	            ResultSet rs = stmt.executeQuery(req);
	            if(!rs.next())
	            {
	                return false;
	            } else
	            {
	                stmt = con.createStatement();
	                req = "UPDATE CollectionsDB SET name='"+item.getName()+"' ,poster='"+item.getPoster()+"' ,year='"+item.getYear()+" WHERE id='"+item.getID()+"';";
	                stmt.executeUpdate(req);
	                return true;
	            }
	        }
}
