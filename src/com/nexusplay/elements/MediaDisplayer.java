package com.nexusplay.elements;

import com.nexusplay.containers.CategoryContainer;
import com.nexusplay.containers.Media;

import java.util.*;

public class MediaDisplayer
{

    public MediaDisplayer(Media elems[])
    {
        categs = new HashMap<String, CategoryContainer>();
        Media amedia[];
        int j = (amedia = elems).length;
        for(int i = 0; i < j; i++)
        {
            Media item = amedia[i];
            if(categs.get(item.getCategory()) == null)
                categs.put(item.getCategory(), new CategoryContainer(item.getCategory()));
            ((CategoryContainer)categs.get(item.getCategory())).addMedia(item);
        }

    }

    public MediaDisplayer(CategoryContainer cat[])
    {
        categs = new HashMap<String, CategoryContainer>();
        CategoryContainer acategorycontainer[];
        int j = (acategorycontainer = cat).length;
        for(int i = 0; i < j; i++)
        {
            CategoryContainer item = acategorycontainer[i];
            categs.put(item.getCategoryName(), item);
        }

    }

    public CategoryContainer[] getCategories(){
    	Collection<CategoryContainer> c = categs.values();
        CategoryContainer totalCategs[] = new CategoryContainer[categs.values().size()];
        int ci = 0;
        for(Iterator<CategoryContainer> iterator = c.iterator(); iterator.hasNext();)
        {
            CategoryContainer x = (CategoryContainer)iterator.next();
            totalCategs[ci] = x;
            ci++;
        }
        return totalCategs;
    }

    private HashMap<String, CategoryContainer> categs;

	public HashMap<String, CategoryContainer> getCategsMap() {
		return categs;
	}
}
