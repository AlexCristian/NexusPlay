package com.nexusplay.containers;

import com.google.gson.*;
import com.nexusplay.security.RandomContainer;
import java.io.*;
import java.math.BigInteger;
import java.net.URL;
import java.net.URLConnection;

/**
 * Contains a video file and all its metadata.
 * @author alex
 *
 */
public class Media
{
	private String name;
    private String id;
    private String jsonResult;
    private String poster;
    private String year;
    private String filename;
    private String category;
    private String collectionID;
    private int published;
    private int season;
    private int episode;
    private JsonObject jsonObject;
    private int views;
    private Gson gson;

    /**
     * Used for instantiating a new Media object that's already
     * been stored in that database.
     * @param mediaName The video's title
     * @param idS Unique video identifier ID
     * @param file URI pointing towards the video's location
     * @param categ The video's category
     * @param published Recognizes whether or not the video is public
     */
    public Media(String mediaName, String idS, String file, String categ, int published)
    {
        name = "";
        poster = "";
        year = "";
        category = "";
        collectionID = "";
        season = 0;
        episode = 0;
        views = 1;
        name = mediaName;
        id = idS;
        filename = file;
        category = categ;
        this.published = published;
    }

    /**
     * Method for obtaining the pure filename of the video.
     * @param filePath URI pointing towards the video
     * @return The filename stripped of its extension
     */
    private static String removeExtension(String filePath)
    {
        File f = new File(filePath);
        if(f.isDirectory())
            return filePath;
        String name = f.getName();
        int lastPeriodPos = name.lastIndexOf('.');
        if(lastPeriodPos <= 0)
        {
            return filePath;
        } else
        {
            File renamed = new File(f.getParent(), name.substring(0, lastPeriodPos));
            return renamed.getPath();
        }
    }

    /**
     * Constructor used for instantiating a new Media object,
     * not yet stored in the database. It automatically attempts
     * to look up the object's meta data on the web while also
     * trying to determine whether the video is part of a collection
     * or not.
     * @param mediaPath URI pointing towards the video
     */
    public Media(String mediaPath)
    {
        name = "";
        poster = "";
        year = "";
        category = "";
        collectionID = "";
        season = 0;
        episode = 0;
        views = 1;
        filename = mediaPath;
        published = 0;
        generateId();
        mediaPath = removeExtension(mediaPath);
        if(mediaPath.lastIndexOf(File.separator) > 0)
            mediaPath = mediaPath.substring(mediaPath.lastIndexOf(File.separator) + 1);
        mediaPath = mediaPath.replace('.', ' ');
        mediaPath = mediaPath.replace('_', ' ');
        mediaPath = mediaPath.replace('-', ' ');
        try
        {
            URL url = new URL((new StringBuilder("http://www.omdbapi.com/?t=")).append(mediaPath.replaceAll(" ", "+")).toString());
            URLConnection connection = url.openConnection();
            connection.addRequestProperty("User-Agent", "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.0)");
            connection.connect();
            StringBuilder builder = new StringBuilder();
            BufferedReader reader = new BufferedReader(new InputStreamReader(connection.getInputStream()));
            String line, jsonResult;
            while((line = reader.readLine()) != null) 
                builder.append(line);
            jsonResult = builder.toString();
            for(; mediaPath.contains(" ") && jsonResult.contentEquals("{\"Response\":\"False\",\"Error\":\"Movie not found!\"}"); mediaPath = mediaPath.substring(0, mediaPath.lastIndexOf(" ")))
            {
                url = new URL((new StringBuilder("http://www.omdbapi.com/?t=")).append(mediaPath.replaceAll(" ", "+")).toString());
                connection = url.openConnection();
                connection.addRequestProperty("User-Agent", "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.0)");
                connection.connect();
                builder = new StringBuilder();
                reader = new BufferedReader(new InputStreamReader(connection.getInputStream()));
                while((line = reader.readLine()) != null) 
                    builder.append(line);
                jsonResult = builder.toString();
            }

            if(mediaPath.contains(" "))
            {
                //jsonResult = jsonResult.substring(1, jsonResult.length() - 1);
                gson = new Gson();
                JsonParser parser = new JsonParser();
                jsonObject = parser.parse(jsonResult).getAsJsonObject();
                name = (String)gson.fromJson(jsonObject.get("Title"), String.class);
                poster = savePoster((String)gson.fromJson(jsonObject.get("Poster"), String.class));
                year = (String)gson.fromJson(jsonObject.get("Year"), String.class);
                category = (String)gson.fromJson(jsonObject.get("Type"), String.class);
                category = matchCategory(category);
            }
        }
        catch(Exception e)
        {
            e.printStackTrace();
        }
        episode = attemptIntParsing(filename, "episode");
        season = attemptIntParsing(filename, "season");
        if(name.equals(""))
            name = attemptEpisodeParsing(filename);
        if(episode != 0)
            category = "TV Shows";
    }
    
    private String matchCategory(String categ){
    	if(categ.equals("movie"))
    		return "Movies";
    	else if(categ.equals("series"))
    		return "TV Shows";
    	return categ;
    }

    /**
     * Attempts to extract the episode's title 
     * from the video's filename.
     * @param mediaName URI pointing towards the video
     * @return Episode title if parsing is successful, void string otherwise
     */
    private String attemptEpisodeParsing(String mediaName)
    {
        String eqName = mediaName;
        if(eqName.contains("-"))
        {
            int start = eqName.indexOf("-") + 1;
            if(eqName.charAt(start) == ' ')
                start++;
            int end;
            for(end = start; Character.isLetterOrDigit(eqName.charAt(end)) || eqName.charAt(end) == '\'' || eqName.charAt(end) == ' '; end++);
            return eqName.substring(start, end);
        } else
        {
            return "";
        }
    }

    /**
     * Attempts to parse from the filename the episode
     * value corresponding to the provided key.
     * @param mediaName URI pointing towards video
     * @param key Key to look for in the filename
     * @return If found, integer corresponding to the given key, 0 otherwise.
     */
    private int attemptIntParsing(String mediaName, String key)
    {
        String eqName = mediaName.toLowerCase();
        if(eqName.contains(key))
        {
            int start = eqName.indexOf(key) + key.length();
            if(eqName.charAt(start) == ' ')
                start++;
            int end;
            for(end = start; Character.isDigit(eqName.charAt(end)); end++);
            try{
            	return Integer.parseInt(eqName.substring(start, end));
            }catch (NumberFormatException e){
            	e.printStackTrace();
            }
        }
        return 0;
    }

    /**
     * Fetches and saves to disk the poster fed by the API.
     * @param imageUrl The poster's URL
     * @return The path towards the saved poster
     * @throws IOException If we don't have permission or space to write to disk, etc.
     */
    public static String savePoster(String imageUrl)
        throws IOException
    {
        String destinationFile;
        long posterId;
        do
        {
            posterId = RandomContainer.getRandom().nextLong();
            destinationFile = SettingsContainer.getAbsolutePosterPath() + File.separator + posterId + ".jpg";
        } while((new File(destinationFile)).exists());
        URL url = new URL(imageUrl);
        InputStream is = url.openStream();
        OutputStream os = new FileOutputStream(destinationFile);
        byte b[] = new byte[2048];
        int length;
        while((length = is.read(b)) != -1) 
            os.write(b, 0, length);
        is.close();
        os.close();
        return SettingsContainer.getPosterSource() + "/" + posterId + ".jpg";
    }
    
    public String getFileFormat(){
    	return filename.substring(filename.lastIndexOf(".")+1);
    }

    /**
     * 
     * @return Path towards poster image
     */
    public String getPoster()
    {
        return poster;
    }

    /**
     * 
     * @return Year of appearance
     */
    public String getYear()
    {
        return year;
    }

    /**
     * 
     * @return The item's name
     */
    public String getName()
    {
        return name;
    }

    /**
     * 
     * @param name The item's new name
     */
    public void setName(String name)
    {
        this.name = name;
    }

    /**
     * Generates a new unique ID for the item
     */
    public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }

    /**
     * 
     * @return The raw JSON response from the API
     */
    public String getJsonResult()
    {
        return jsonResult;
    }

    /**
     * 
     * @return The item's unique ID
     */
    public String getId()
    {
        return id;
    }

    /**
     * 
     * @param id The item's new unique ID
     */
    public void setId(String id)
    {
        this.id = id;
    }

    /**
     * 
     * @param poster The path towards the new poster image
     */
    public void setPoster(String poster)
    {
        this.poster = poster;
    }

    /**
     * Checks whether or not this item has been published.
     * @return The according boolean value
     */
    public int getPublished()
    {
        return published;
    }

    /**
     * Sets the item's published status
     * @param published The according boolean value
     */
    public void setPublished(int published)
    {
        this.published = published;
    }

    /**
     * 
     * @param year The new year of appearance
     */
    public void setYear(String year)
    {
        this.year = year;
    }

    /**
     * 
     * @return The path towards the video file
     */
    public String getFilename()
    {
        return filename;
    }

    /**
     * 
     * @param filename The new path towards the video file
     */
    public void setFilename(String filename)
    {
        this.filename = filename;
    }

    /**
     * 
     * @return The number of views
     */
    public int getViews()
    {
        return views;
    }

    /**
     * 
     * @param views The new number of views
     */
    public void setViews(int views)
    {
        this.views = views;
    }

    /**
     * 
     * @return The category the Media object is part of
     */
    public String getCategory()
    {
        return category;
    }

    /**
     * 
     * @param category The new category the Media object is to be part of
     */
    public void setCategory(String category)
    {
        this.category = category;
    }

    /**
     * 
     * @return The season number to which the video pertains
     */
    public int getSeason()
    {
        return season;
    }

    /**
     * 
     * @param season The new season number to which the video pertains
     */
    public void setSeason(int season)
    {
        this.season = season;
    }

    /**
     * 
     * @return The item's episode number
     */
    public int getEpisode()
    {
        return episode;
    }

    /**
     * 
     * @param episode The item's new episode number
     */
    public void setEpisode(int episode)
    {
        this.episode = episode;
    }

    /**
     * 
     * @return The unique ID of the collection the item pertains to
     */
    public String getCollectionID()
    {
        return collectionID;
    }

    /**
     * 
     * @param collectionID The unique ID of the new collection the item pertains to
     */
    public void setCollectionID(String collectionID)
    {
        this.collectionID = collectionID;
    }
}
