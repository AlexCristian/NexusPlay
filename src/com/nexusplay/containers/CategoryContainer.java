package com.nexusplay.containers;

import java.util.ArrayList;

/**
 * Container class meant to bring together media falling
 * within the same category, i.e. Movies, TV Shows, etc.
 * @author alex
 *
 */
public class CategoryContainer
{

	/**
	 * The CategoryContainer constructor initializes the
	 * object and names the category according to the name
	 * parameter.
	 * @param name Sets the category's name
	 */
    public CategoryContainer(String name)
    {
        media = new ArrayList<Media>();
        categoryName = name;
    }

    /**
     * Adds a Media object to the CategoryContainer.
     * @param media Member to be added to the category
     * @see Media
     */
    public void addMedia(Media media)
    {
        this.media.add(media);
    }

    /**
     * Removes a Media object from the CategoryContainer.
     * @param media Member to be looked for and deleted from the category
     * @see Media
     */
    public void removeMedia(Media media)
    {
        this.media.remove(media);
    }

    /**
     * 
     * @return The category's name
     */
    public String getCategoryName()
    {
        return categoryName;
    }

    /**
     * 
     * @param categoryName The new category name
     */
    public void setCategoryName(String categoryName)
    {
        this.categoryName = categoryName;
    }

    /**
     * 
     * @return All media contained within the category
     */
    public ArrayList<Media> getMedia()
    {
        return media;
    }
    
    /**
     * 
     * @param media Media collection to replace the one currently held in the container.
     */
    public void setMedia(ArrayList<Media> media)
    {
        this.media = media;
    }

    private String categoryName;
    private ArrayList<Media> media;
}
