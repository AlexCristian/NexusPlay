package com.nexusplay.containers;

import java.util.ArrayList;

// Referenced classes of package com.nexusplay.containers:
//            Media

public class CategoryContainer
{

    public CategoryContainer(String name)
    {
        media = new ArrayList();
        categoryName = name;
    }

    public void addMedia(Media media)
    {
        this.media.add(media);
    }

    public void removeMedia(Media media)
    {
        this.media.remove(media);
    }

    public String getCategoryName()
    {
        return categoryName;
    }

    public void setCategoryName(String categoryName)
    {
        this.categoryName = categoryName;
    }

    public ArrayList getMedia()
    {
        return media;
    }

    public void setMedia(ArrayList media)
    {
        this.media = media;
    }

    private String categoryName;
    private ArrayList media;
}
