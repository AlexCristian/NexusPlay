package com.nexusplay.db;

import com.nexusplay.containers.Media;
import java.sql.*;
import java.util.ArrayList;

public class MediaDatabase
{
	private static final String tableDefinition = "CREATE TABLE IF NOT EXISTS MediaDB(name VARCHAR(50), filename VARCHAR(100), poster VARCHAR(100), id VARCHAR(1000), year VARCHAR(300), views INT(1), category VARCHAR(50), season INT(1), episode INT(1), collectionid VARCHAR(1000), published INT(1));";
	
    public MediaDatabase()
    {
    }

    private static Connection getConnection() throws SQLException{
    	Statement stmt = null;
        Connection con = ConnectionManager.getConnection();
        stmt = con.createStatement();
        stmt.executeUpdate(tableDefinition);
        return con;
    }
    public static void pushMedia(Media item)
        throws SQLException, Exception
    {
        Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = (new StringBuilder("SELECT * FROM MediaDB WHERE name='")).append(item.getName()).append("';").toString();
        ResultSet rs = stmt.executeQuery(req);
        if(rs.next())
            throw new Exception("Duplicate item!");
        boolean pp = true;
        do
        {
            pp = false;
            stmt = con.createStatement();
            item.generateId();
            req = (new StringBuilder("SELECT * FROM MediaDB WHERE id='")).append(item.getId()).append("';").toString();
            rs = stmt.executeQuery(req);
            if(rs.next())
                pp = true;
        } while(pp);
        stmt = con.createStatement();
        req = "INSERT INTO MediaDB VALUES ('"+item.getName()+"' , '"+item.getFilename()+"' , '"+item.getPoster()+"' , '"+item.getId()+"' , '"+item.getYear()+"' , 1, '"+item.getCategory()+"' , "+ item.getSeason() +" , " + item.getEpisode() +" , '" + item.getCollectionID() +"' , 0);";
        stmt.executeUpdate(req);
    }

    public static void deleteMedia(Media item)
        throws SQLException
    {
        Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = (new StringBuilder("DELETE FROM MediaDB WHERE id='")).append(item.getId()).append("';").toString();
        stmt.executeUpdate(req);
    }

    public static Media getMediaById(String id)
        throws SQLException
    {
        Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB WHERE ");
        req = (new StringBuilder(String.valueOf(req))).append("id = '").append(id).append("';").toString();
        ResultSet rs = stmt.executeQuery(req);
        if(!rs.next())
        {
            return null;
        } else
        {
            Media item = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
            item.setPoster(rs.getString("poster"));
            item.setCollectionID(rs.getString("collectionid"));
            item.setEpisode(rs.getInt("episode"));
            item.setSeason(rs.getInt("season"));
            item.setYear(rs.getString("year"));
            item.setViews(rs.getInt("views"));
            return item;
        }
    }

    public static boolean checkExists(String filename)
        throws SQLException
    {
        Connection con = getConnection();
        Statement stmt = null;
        stmt = con.createStatement();
        String req = (new StringBuilder("SELECT * FROM MediaDB WHERE filename='")).append(filename).append("';").toString();
        ResultSet rs = stmt.executeQuery(req);
        return rs.next();
    }

    public static boolean replaceMedia(Media item)
        throws SQLException
    {
        Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB WHERE ");
        req = (new StringBuilder(String.valueOf(req))).append("id like '%").append(item.getId()).append("%';").toString();
        ResultSet rs = stmt.executeQuery(req);
        if(!rs.next())
        {
            return false;
        } else
        {
            stmt = con.createStatement();
            req = "UPDATE MediaDB SET name='"+item.getName()+"' ,filename='"+item.getFilename()+"' ,"+"poster='"+item.getPoster()+"' ,"+"category='"+item.getCategory()+"' ,year='"+item.getYear()+"', collectionid='" + item.getCollectionID() + "', season=" + item.getSeason() + ", episode=" + item.getEpisode() + ", published=" + item.getPublished()+" WHERE id='"+item.getId()+"';";
            stmt.executeUpdate(req);
            return true;
        }
    }
    
    public static boolean publishMedia(String id)
            throws SQLException
        {
            Statement stmt = null;
            Connection con = getConnection();
            stmt = con.createStatement();
            String req = new String("SELECT * FROM MediaDB WHERE ");
            req = (new StringBuilder(String.valueOf(req))).append("id like '%").append(id).append("%';").toString();
            ResultSet rs = stmt.executeQuery(req);
            if(!rs.next())
            {
                return false;
            } else
            {
                stmt = con.createStatement();
                req = "UPDATE MediaDB SET published=1 WHERE id="+id+";";
                stmt.executeUpdate(req);
                return true;
            }
        }

    public static Media[] getPopularMedia(int size)
        throws SQLException
    {
        Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB ORDER BY views DESC;");
        ResultSet rs = stmt.executeQuery(req);
        ArrayList raw = new ArrayList();
        for(int i = 1; i < size && rs.next(); i++)
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
    
    public static Media[] getUnpublishedMedia() throws SQLException
        {
            Statement stmt = null;
            Connection con = getConnection();
            stmt = con.createStatement();
            String req = new String("SELECT * FROM MediaDB WHERE ");
            req = (new StringBuilder(String.valueOf(req))).append("published = 0;").toString();
            ResultSet rs = stmt.executeQuery(req);
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

    public static Media[] getRecentMedia(int size)
        throws SQLException
    {
        Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB;");
        ResultSet rs = stmt.executeQuery(req);
        ArrayList raw = new ArrayList();
        rs.setFetchDirection(1001);
        for(int i = 1; i < size && rs.next(); i++)
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

    public static Media[] searchMedia(String request, int resultCap)
        throws SQLException
    {
        ArrayList raw = new ArrayList();
        Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB WHERE ");
        req = (new StringBuilder(String.valueOf(req))).append("name like '%").append(request).append("%';").toString();
        Media item; int k=0;
        for(ResultSet rs = stmt.executeQuery(req); rs.next() && k<=resultCap; raw.add(item), k++)
        {
            item = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
            item.setPoster(rs.getString("poster"));
            item.setYear(rs.getString("year"));
            item.setCollectionID(rs.getString("collectionid"));
            item.setEpisode(rs.getInt("episode"));
            item.setSeason(rs.getInt("season"));
            item.setViews(rs.getInt("views"));
        }

        Media result[] = new Media[raw.size()];
        for(int i = 0; i < raw.size(); i++)
            result[i] = (Media)raw.get(i);

        return result;
    }
    
    public static ArrayList<ArrayList<Media>> getCollectionEpisodes(String collectionID) throws SQLException{
    	ArrayList<ArrayList<Media>> episodes = new ArrayList<ArrayList<Media>>();
    	Statement stmt = null;
        Connection con = getConnection();
        stmt = con.createStatement();
        String req = new String("SELECT * FROM MediaDB WHERE collectionid='" + collectionID + "' ORDER BY season ASC, episode ASC;");
        ResultSet rs = stmt.executeQuery(req);
        int currentSeason = -1;
        while(rs.next())
        {
            Media temp = new Media(rs.getString("name"), rs.getString("id"), rs.getString("filename"), rs.getString("category"), rs.getInt("published"));
            temp.setPoster(rs.getString("poster"));
            temp.setCollectionID(rs.getString("collectionid"));
            temp.setEpisode(rs.getInt("episode"));
            temp.setSeason(rs.getInt("season"));
            temp.setViews(rs.getInt("views"));
            temp.setYear(rs.getString("year"));
            if(temp.getSeason()!=currentSeason){
            	currentSeason = temp.getSeason();
            	episodes.add(currentSeason, new ArrayList<Media>());
            }
            episodes.get(currentSeason).add(temp);
        }
        return episodes;
    }

}
