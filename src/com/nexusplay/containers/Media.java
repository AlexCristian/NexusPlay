package com.nexusplay.containers;

import com.google.gson.*;
import com.nexusplay.security.RandomContainer;
import java.io.*;
import java.math.BigInteger;
import java.net.*;
import java.security.SecureRandom;

// Referenced classes of package com.nexusplay.containers:
//            SettingsContainer

public class Media
{
	
	private String name;
    private String id;
    private String jsonResult;
    private String poster;
    private String year;
    private String filename;
    private String category, collectionID="";
    private int published, season=0, episode=0;
    private JsonObject jsonObject;
    private int views;
    private Gson gson;

    //in case you rename your media
    public Media(String mediaName, String idS, String categ, int published)
    {
        views = 1;
        name = mediaName;
        id = idS;
        filename = idS;
        category = categ;
        this.published = published;
    }

    public Media(String mediaName, String idS, String file, String categ, int published)
    {
        views = 1;
        name = mediaName;
        id = idS;
        filename = file;
        category = categ;
        this.published = published;
    }

    public static String removeExtention(String filePath)
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

    public Media(String mediaName)
    {
        views = 1;
        filename = mediaName;
        published = 0;
        mediaName = removeExtention(mediaName);
        if(mediaName.lastIndexOf(File.separator) > 0)
            mediaName = mediaName.substring(mediaName.lastIndexOf(File.separator) + 1);
        mediaName = mediaName.replace('.', ' ');
        mediaName = mediaName.replace('_', ' ');
        mediaName = mediaName.replace('-', ' ');
        try
        {
            URL url = new URL((new StringBuilder("http://imdbapi.org/?q=")).append(mediaName.replaceAll(" ", "+")).toString());
            URLConnection connection = url.openConnection();
            connection.addRequestProperty("User-Agent", "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.0)");
            connection.connect();
            StringBuilder builder = new StringBuilder();
            BufferedReader reader = new BufferedReader(new InputStreamReader(connection.getInputStream()));
            String line;
            while((line = reader.readLine()) != null) 
                builder.append(line);
            for(jsonResult = builder.toString(); !mediaName.contains(" ") && jsonResult.contentEquals("{\"code\":404, \"error\":\"Film not found\"}"); jsonResult = builder.toString())
            {
                mediaName = mediaName.substring(0, mediaName.lastIndexOf(" "));
                url = new URL((new StringBuilder("http://imdbapi.org/?q=")).append(mediaName.replaceAll(" ", "+")).toString());
                connection = url.openConnection();
                connection.addRequestProperty("User-Agent", "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.0)");
                connection.connect();
                builder = new StringBuilder();
                reader = new BufferedReader(new InputStreamReader(connection.getInputStream()));
                while((line = reader.readLine()) != null) 
                    builder.append(line);
            }

            if(!mediaName.contains(" ")){
            	name = "";
                poster = "";
                year = "";
                category = "";
            }else{
	            jsonResult = jsonResult.substring(1, jsonResult.length() - 1);
	            gson = new Gson();
	            JsonParser parser = new JsonParser();
	            jsonObject = parser.parse(jsonResult).getAsJsonObject();
	            name = (String)gson.fromJson(jsonObject.get("title"), String.class);
	            poster = savePoster((String)gson.fromJson(jsonObject.get("poster"), String.class), filename);
	            year = (String)gson.fromJson(jsonObject.get("year"), String.class);
	            category = (String)gson.fromJson(jsonObject.get("type"), String.class);
            }
        }
        catch(MalformedURLException e)
        {
            e.printStackTrace();
        }
        catch(IOException e)
        {
            e.printStackTrace();
        }
    }

    public static String savePoster(String imageUrl, String mediaName)
        throws IOException
    {
        String destinationFile;
        long posterId;
        do
        {
            posterId = RandomContainer.getRandom().nextLong();
            destinationFile = (new StringBuilder(String.valueOf(SettingsContainer.getAbsolutePosterPath()))).append(File.separator).append(posterId).append(".jpg").toString();
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
        return (new StringBuilder(String.valueOf(SettingsContainer.getPosterSource()))).append("/").append(posterId).append(".jpg").toString();
    }

    public void overwriteChanges()
    {
    }

    public String getPoster()
    {
        return poster;
    }

    public String getYear()
    {
        return year;
    }

    public String getName()
    {
        return name;
    }

    public void setName(String name)
    {
        this.name = name;
    }

    public void generateId()
    {
        id = (new BigInteger(130, RandomContainer.getRandom())).toString(32);
    }

    public String getJsonResult()
    {
        return jsonResult;
    }

    public String getId()
    {
        return id;
    }

    public void setId(String id)
    {
        this.id = id;
    }

    public void setPoster(String poster)
    {
        this.poster = poster;
    }

    public int getPublished() {
		return published;
	}

	public void setPublished(int published) {
		this.published = published;
	}

	public void setYear(String year)
    {
        this.year = year;
    }

    public String getFilename()
    {
        return filename;
    }

    public void setFilename(String filename)
    {
        this.filename = filename;
    }

    public int getViews()
    {
        return views;
    }

    public void setViews(int views)
    {
        this.views = views;
    }

    public String getCategory()
    {
        return category;
    }

    public void setCategory(String category)
    {
        this.category = category;
    }

	public int getSeason() {
		return season;
	}

	public void setSeason(int season) {
		this.season = season;
	}

	public int getEpisode() {
		return episode;
	}

	public void setEpisode(int episode) {
		this.episode = episode;
	}

	public String getCollectionID() {
		return collectionID;
	}

	public void setCollectionID(String collectionID) {
		this.collectionID = collectionID;
	}

}
