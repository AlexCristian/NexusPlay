package com.nexusplay.db;

import com.nexusplay.containers.SettingsContainer;
import java.io.PrintStream;
import java.sql.*;

public class ConnectionManager
{

    public ConnectionManager()
    {
    }

    public static void createConnection(String url, String port, String dbName, String userName, String password)
    {
        isRunning = true;
        String driver = "com.mysql.jdbc.Driver";
        try
        {
            Class.forName(driver).newInstance();
        }
        catch(Exception e)
        {
            System.out.println("Instantiation exception: Driver not found!");
            e.printStackTrace();
        }
        try
        {
            conn = DriverManager.getConnection((new StringBuilder("jdbc:mysql://")).append(url).append(":").append(port).append("/").append(dbName).toString(), userName, password);
        }
        catch(Exception e)
        {
            e.printStackTrace();
        }
        isRunning = false;
    }

    public static void closeConnection()
        throws SQLException
    {
        if(conn != null)
            conn.close();
    }

    public static Connection getConnection()
    {
        while(conn == null) 
            try
            {
                if(!isRunning)
                    createConnection(SettingsContainer.getDbURL(), SettingsContainer.getDbPort(), SettingsContainer.getDbName(), SettingsContainer.getDbUserName(), SettingsContainer.getDbPass());
                Thread.sleep(100L);
            }
            catch(InterruptedException e)
            {
                e.printStackTrace();
            }
        return conn;
    }

    private static Connection conn = null;
    private static boolean isRunning = false;

}
