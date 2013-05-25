package com.nexusplay.elements;

import com.nexusplay.containers.CategoryContainer;
import com.nexusplay.containers.Media;

import java.io.IOException;
import java.util.*;

import javax.servlet.ServletException;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

public class MediaDisplayer
{

    public MediaDisplayer(Media elems[])
    {
        categs = new HashMap();
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
        categs = new HashMap();
        CategoryContainer acategorycontainer[];
        int j = (acategorycontainer = cat).length;
        for(int i = 0; i < j; i++)
        {
            CategoryContainer item = acategorycontainer[i];
            categs.put(item.getCategoryName(), item);
        }

    }

    public CategoryContainer[] getCategories(){
    	Collection c = categs.values();
        CategoryContainer totalCategs[] = new CategoryContainer[categs.values().size()];
        int ci = 0;
        for(Iterator iterator = c.iterator(); iterator.hasNext();)
        {
            CategoryContainer x = (CategoryContainer)iterator.next();
            totalCategs[ci] = x;
            ci++;
        }
        return totalCategs;
    }

    private HashMap categs;

	public HashMap getCategsMap() {
		return categs;
	}
}
