package com.nexusplay.containers;

/**
 * This class contains the various connection information and
 * settings necessary for Nexus to function.
 * @author alex
 *
 */
public class SettingsContainer
{
	
	private final static String absoluteMediaPath = "/home/alex/Media";
	//private final static String absoluteMediaPath = "C:/media/downloads";
    private final static String mediaSource = "/media";
    private final static String posterFolder = "posters";
    private final static String posterSource = mediaSource + "/" + posterFolder;
    private final static String absolutePosterPath = absoluteMediaPath + "/" + posterFolder;
    
    private final static String subtitleFolder = "subtitles";
    private final static String subtitleSource = mediaSource + "/" + subtitleFolder;
    private final static String absoluteSubtitlePath = absoluteMediaPath + "/" + subtitleFolder;
    
    private final static String administratorNickname = "admin";

    private final static String dbURL = "localhost";
    private final static String dbPort = "3306";
    private final static String dbName = "NexusPlayDB";
    private final static String dbUserName = "nexusplay";
    private final static String dbPass = "nexusplay";

    /**
     * 
     * @return The web location where the media can be accessed 
     */
    public static String getMediaSource()
    {
        return mediaSource;
    }

    /**
     * 
     * @return The nickname with administrator rights
     */
	public static String getAdministratorNickname() {
		return administratorNickname;
	}

	/**
	 * 
	 * @return The location of the media folder on disk
	 */
	public static String getAbsoluteMediaPath() {
		return absoluteMediaPath;
	}

	/**
	 * 
	 * @return The folder where the posters are located
	 */
	public static String getPosterFolder() {
		return posterFolder;
	}

	/**
	 * 
	 * @return The web location where the posters are located
	 */
	public static String getPosterSource() {
		return posterSource;
	}

	/**
	 * 
	 * @return The location of the poster folder on disk
	 */
	public static String getAbsolutePosterPath() {
		return absolutePosterPath;
	}

	/**
	 * 
	 * @return The SQL server's URL
	 */
	public static String getDbURL() {
		return dbURL;
	}

	/**
	 * 
	 * @return The SQL server's port
	 */
	public static String getDbPort() {
		return dbPort;
	}

	/**
	 * 
	 * @return The database name
	 */
	public static String getDbName() {
		return dbName;
	}

	/**
	 * 
	 * @return The server's user name
	 */
	public static String getDbUserName() {
		return dbUserName;
	}

	/**
	 * 
	 * @return The server's password
	 */
	public static String getDbPass() {
		return dbPass;
	}

	/**
	 * @return the subtitlefolder
	 */
	public static String getSubtitleFolder() {
		return subtitleFolder;
	}

	/**
	 * @return the subtitlesource
	 */
	public static String getSubtitleSource() {
		return subtitleSource;
	}

	/**
	 * @return the absolutesubtitlepath
	 */
	public static String getAbsoluteSubtitlePath() {
		return absoluteSubtitlePath;
	}
}
