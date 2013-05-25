package com.nexusplay.security;

import java.security.SecureRandom;

public class RandomContainer
{

    public RandomContainer()
    {
    }

    public static SecureRandom getRandom()
    {
        return random;
    }

    private static SecureRandom random = new SecureRandom();

}
