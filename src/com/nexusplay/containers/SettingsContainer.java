package com.nexusplay.containers;

import java.io.File;

public class SettingsContainer
{
	
	private static String absoluteMediaPath = "/media/storage";
	//private static String absoluteMediaPath = "C:/media-downloads";
    private static String mediaSource = "/media";
    private static String posterFolder = "posters";
    private static String posterSource = mediaSource + "/" + posterFolder;
    private static String absolutePosterPath = absoluteMediaPath + "/" + posterFolder;
    private static String administratorNickname = "admin";

    private static String dbURL = "localhost";
    private static String dbPort = "3306";
    private static String dbName = "NexusPlayDB";
    private static String dbUserName = "nexusplay";
    private static String dbPass = "nexusplay";

    public static String getMediaSource()
    {
        return mediaSource;
    }

    public static void setMediaSource(String mediaSource)
    {
        mediaSource = mediaSource;
    }

    public static String getDbURL()
    {
        return dbURL;
    }

    public static void setDbURL(String dbURL)
    {
        dbURL = dbURL;
    }

    public static String getDbPort()
    {
        return dbPort;
    }

    public static void setDbPort(String dbPort)
    {
        dbPort = dbPort;
    }

    public static String getDbName()
    {
        return dbName;
    }

    public static void setDbName(String dbName)
    {
        dbName = dbName;
    }

    public static String getDbUserName()
    {
        return dbUserName;
    }

    public static void setDbUserName(String dbUserName)
    {
        dbUserName = dbUserName;
    }

    public static String getDbPass()
    {
        return dbPass;
    }

    public static void setDbPass(String dbPass)
    {
        dbPass = dbPass;
    }

    public static String getAbsoluteMediaPath()
    {
        return absoluteMediaPath;
    }

    public static void setAbsoluteMediaPath(String absoluteMediaPath)
    {
        absoluteMediaPath = absoluteMediaPath;
    }

    public static String getAbsolutePosterPath()
    {
        return absolutePosterPath;
    }

    public static void setAbsolutePosterPath(String absolutePosterPath)
    {
        absolutePosterPath = absolutePosterPath;
    }

    public static String getPosterSource()
    {
        return posterSource;
    }

    public static void setPosterSource(String posterSource)
    {
        posterSource = posterSource;
    }

	public static String getAdministratorNickname() {
		return administratorNickname;
	}

	public static void setAdministratorNickname(String administratorNickname) {
		SettingsContainer.administratorNickname = administratorNickname;
	}
}
