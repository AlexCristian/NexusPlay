package com.nexusplay.db;

import com.nexusplay.containers.Media;
import com.nexusplay.containers.SettingsContainer;

import java.io.File;
import java.sql.SQLException;

/**
 * The UpdaterAgent class is designed to continuously search for newly
 * added media in the selected source folder.
 * @author alex
 *
 */
public class UpdaterAgent extends Thread
{
    private String path = SettingsContainer.getAbsoluteMediaPath();;
    private File mediaFolder;

    /**
     * Main thread loop method.
     */
    public void run()
    {
        File x = new File(SettingsContainer.getAbsoluteMediaPath());
        if(!x.exists())
            x.mkdirs();
        x = new File(SettingsContainer.getAbsolutePosterPath());
        if(!x.exists())
            x.mkdirs();
        do
            try
            {
                mediaFolder = new File(path);
                checkFolder(mediaFolder, "");
                Thread.yield();
                Thread.sleep(300000);
            }
            catch(Exception E)
            {
                E.printStackTrace();
            }
        while(!this.isInterrupted());
        x=null; nullify();
    }

    /**
     * Set variables to null in order to prevent memory leaks.
     */
    private void nullify(){
    	mediaFolder=null; path=null;
    }
    
    /**
     * Check the parameter location for undiscovered media.
     * @param mediaFolder The current folder to search in
     * @param parentFolder The parent folder containing the current location
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    private void checkFolder(File mediaFolder, String parentFolder)
        throws SQLException
    {
        File files[] = mediaFolder.listFiles();
        
        File afile[];
        int j = (afile = files).length;
        for(int i = 0; i < j; i++)
        {
            File file = afile[i];
            if(file.isDirectory())
                checkFolder(file, parentFolder + "/" + file.getName());
            else
            if(isSupportedFormat(file) && !MediaDatabase.checkExists(parentFolder + "/" + file.getName()))
            {
                Media newMedia = new Media(parentFolder + "/" + file.getName());
                try {
					MediaDatabase.pushMedia(newMedia);
				} catch (Exception e) {
					//should not happen since we already if the item existed
					e.printStackTrace();
					System.err.println("More than one thread is adding media to the database!");
				}
            }
        }

    }

    /**
     * Checks whether a file has a supported format or not.
     * @param file The file to be inspected
     * @return True or false if the file isn't supported
     */
    private boolean isSupportedFormat(File file)
    {
        String name = file.getName();
        int lastPeriodPos = name.lastIndexOf('.');
        String extension = name.substring(lastPeriodPos);
        return extension.equals(".mp4");
    }

}
