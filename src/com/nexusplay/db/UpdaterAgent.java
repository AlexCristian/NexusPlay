package com.nexusplay.db;

import com.nexusplay.containers.Media;
import com.nexusplay.containers.SettingsContainer;
import java.io.File;
import java.sql.SQLException;

// Referenced classes of package com.nexusplay.db:
//            MediaDatabase

public class UpdaterAgent extends Thread
{
    private String path = SettingsContainer.getAbsoluteMediaPath();;
    private File mediaFolder;

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
                Thread.sleep(0x493e0L);
            }
            catch(Exception E)
            {
                E.printStackTrace();
            }
        while(!this.isInterrupted());
        x=null; nullify();
    }

    private void nullify(){
    	mediaFolder=null; path=null;
    }
    private void checkFolder(File mediaFolder, String parentFolder)
        throws SQLException, Exception
    {
        File files[] = mediaFolder.listFiles();
        
        File afile[];
        int j = (afile = files).length;
        for(int i = 0; i < j; i++)
        {
            File file = afile[i];
            if(file.isDirectory())
                checkFolder(file, (new StringBuilder(String.valueOf(parentFolder))).append("/").append(file.getName()).toString());
            else
            if(isSupportedFormat(file) && !MediaDatabase.checkExists((new StringBuilder(String.valueOf(parentFolder))).append("/").append(file.getName()).toString()))
            {
                Media newMedia = new Media((new StringBuilder(String.valueOf(parentFolder))).append("/").append(file.getName()).toString());
                MediaDatabase.pushMedia(newMedia);
            }
        }

    }

    private boolean isSupportedFormat(File file)
    {
        String name = file.getName();
        int lastPeriodPos = name.lastIndexOf('.');
        String extension = name.substring(lastPeriodPos);
        return extension.equals(".mp4");
    }

}
