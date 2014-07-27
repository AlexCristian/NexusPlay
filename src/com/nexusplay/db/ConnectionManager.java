package com.nexusplay.db;

import com.nexusplay.containers.SettingsContainer;
import java.sql.*;

/**
 * This class dispatches connections to the other database classes,
 * being the only class that interfaces directly with the JDBC driver.
 * @author alex
 *
 */
public class ConnectionManager
{

    public ConnectionManager()
    {
    }

    /**
     * Creates a JDBC connection with the specified parameters.
     * @param url Path toward the server
     * @param port Server SQL port
     * @param dbName The database's name
     * @param userName User name to be used
     * @param password The password associated to the user name
     */
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
            conn = DriverManager.getConnection("jdbc:mysql://"+url+":"+port+"/"+dbName+"?useUnicode=true&characterEncoding=UTF-8", userName, password);
        }
        catch(Exception e)
        {
            e.printStackTrace();
        }
        isRunning = false;
    }

    /**
     * Closes the JDBC connection.
     * @throws SQLException Thrown if the database is not accessible to us for whatever reason
     */
    public static void closeConnection()
        throws SQLException
    {
        if(conn != null)
            conn.close();
    }

    /**
     * Creates a connection using the stored settings and returns a non-null
     * instance of a Connection object.
     * @return
     */
    public static Connection getConnection()
    {
        while(conn == null) 
            try
            {
                if(!isRunning)
                    createConnection(SettingsContainer.getDbURL(), SettingsContainer.getDbPort(), SettingsContainer.getDbName(), SettingsContainer.getDbUserName(), SettingsContainer.getDbPass());
                //TODO Wait time is server-dependent, does not guarantee a non-null Connection instance.
                Thread.sleep(1000);
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
