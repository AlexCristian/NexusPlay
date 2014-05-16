package com.nexusplay.security;

import java.io.IOException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Enumeration;

import javax.servlet.Filter;
import javax.servlet.FilterChain;
import javax.servlet.FilterConfig;
import javax.servlet.ServletException;
import javax.servlet.ServletRequest;
import javax.servlet.ServletResponse;
import javax.servlet.annotation.WebFilter;
import javax.servlet.http.HttpServletRequest;

/**
 * Servlet Filter implementation class QuerySafetyFilter
 */
@WebFilter("/QuerySafetyFilter")
public class QuerySafetyFilter implements Filter {

    /**
     * Default constructor. 
     */
    public QuerySafetyFilter() {
    	
    }

	/**
	 * @see Filter#destroy()
	 */
	public void destroy() {
		
	}

	/**
	 * @see Filter#doFilter(ServletRequest, ServletResponse, FilterChain)
	 */
	public void doFilter(ServletRequest request, ServletResponse response, FilterChain chain) throws IOException, ServletException {

		try{
			ArrayList<String> params = Collections.list(request.getParameterNames());
			for(String parameter : params){
				QuerySanitizer.sanitizeQueryString(request.getParameter(parameter));
			}
			
			// pass the request along the filter chain
			chain.doFilter(request, response);
		}catch(Exception e){
			response.getWriter().write("Invalid characters present! Please restate your query.");
		}
	}

	/**
	 * @see Filter#init(FilterConfig)
	 */
	public void init(FilterConfig fConfig) throws ServletException {
		
	}

}