package com.nexusplay.security;

import javax.servlet.ServletContextEvent;
import javax.servlet.ServletContextListener;

import com.nexusplay.db.UpdaterAgent;

public class SystemOverlord implements ServletContextListener{

	private Thread updater;
	@Override
	public void contextDestroyed(ServletContextEvent arg0) {
		updater.interrupt();
		
	}

	@Override
	public void contextInitialized(ServletContextEvent arg0) {
		updater = new UpdaterAgent();
		updater.start();
	}
 
}
