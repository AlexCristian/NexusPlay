package com.nexusplay.security;

public class QuerySanitizer {

	private final static String allowedCharacters="abcdefghijklmnopqrstuvwxyz"
			+ "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
			+ "1234567890"
			+ "!?:@,._- ";
	public static String sanitizeQueryString(String s) throws Exception{
		s=s.trim();
		for(int i=0; i< s.length(); i++){
			if(allowedCharacters.indexOf(s.charAt(i))==-1){
				throw new Exception("Invalid characters present! Please restate your query.");
			}
		}
		return s;
	}
}
